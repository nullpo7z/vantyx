package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/nullpo7z/vantyx/internal/secret"
)

// TOTPIssuer is the issuer label shown in authenticator apps.
const TOTPIssuer = "Vantyx"

// recoveryCodeCount is how many one-time recovery codes are issued when
// TOTP is enabled.
const recoveryCodeCount = 8

var (
	// ErrTOTPNotEnabled is returned when an operation needs an enabled
	// second factor and the user has none.
	ErrTOTPNotEnabled = errors.New("totp not enabled")
	// ErrTOTPAlreadyEnabled is returned by SavePending when the user
	// already has an active second factor (disable it first).
	ErrTOTPAlreadyEnabled = errors.New("totp already enabled")
	// ErrTOTPNoPending is returned by Confirm when enrolment was never
	// started (or already confirmed).
	ErrTOTPNoPending = errors.New("no pending totp enrolment")
	// ErrTOTPInvalidCode is returned when a code / recovery code does
	// not verify.
	ErrTOTPInvalidCode = errors.New("invalid verification code")
)

// TOTPStatus is the per-user second-factor state.
type TOTPStatus struct {
	// Enabled is true once the first code was confirmed.
	Enabled bool
	// Pending is true while enrolment has started but is unconfirmed.
	Pending bool
	// RecoveryCodesLeft counts the unused recovery codes.
	RecoveryCodesLeft int
	ConfirmedAt       time.Time
}

// TOTPStore persists per-user TOTP secrets and recovery codes.
type TOTPStore interface {
	// Status reports the user's second-factor state (never an error for
	// a user without a row: Enabled=false, Pending=false).
	Status(ctx context.Context, userID string) (TOTPStatus, error)
	// Enabled is a convenience for Status(...).Enabled that swallows
	// errors as false (used on hot auth paths).
	Enabled(ctx context.Context, userID string) bool
	// BeginEnrolment generates a fresh secret for the user and stores it
	// unconfirmed. Returns the otpauth:// URL (and the secret it embeds)
	// for the authenticator app. Fails with ErrTOTPAlreadyEnabled while a
	// confirmed factor exists.
	BeginEnrolment(ctx context.Context, userID, accountName string) (otpauthURL, secretB32 string, err error)
	// Confirm verifies code against the pending secret, enables the
	// factor and returns the freshly generated recovery codes (plain,
	// shown exactly once).
	Confirm(ctx context.Context, userID, code string) ([]string, error)
	// Verify checks a TOTP code -- or, when it does not match, an unused
	// recovery code (which is then consumed) -- against the user's
	// enabled factor. Returns usedRecovery=true when a recovery code was
	// consumed.
	Verify(ctx context.Context, userID, code string) (usedRecovery bool, err error)
	// Disable removes the user's factor (confirmed or pending).
	Disable(ctx context.Context, userID string) error
}

// SQLiteTOTPStore implements TOTPStore on the user_totp table.
type SQLiteTOTPStore struct {
	db  *sql.DB
	key []byte
}

// NewSQLiteTOTPStore creates a store; key encrypts secrets at rest
// (VANTYX_SSH_PASSWORD_ENCRYPTION_KEY).
func NewSQLiteTOTPStore(db *sql.DB, key []byte) *SQLiteTOTPStore {
	return &SQLiteTOTPStore{db: db, key: key}
}

// sealSecret encrypts the base32 secret at rest. Without a key (only
// possible under VANTYX_ALLOW_PLAINTEXT_SECRETS=1, never in production)
// the secret is stored as-is, mirroring how the other stores behave.
func (s *SQLiteTOTPStore) sealSecret(secretB32 string) (string, error) {
	if len(s.key) == 0 {
		return secretB32, nil
	}
	return secret.Encrypt(s.key, secretB32)
}

func (s *SQLiteTOTPStore) openSecret(stored string) (string, error) {
	if len(s.key) == 0 {
		return stored, nil
	}
	return secret.Decrypt(s.key, stored)
}

func (s *SQLiteTOTPStore) Status(ctx context.Context, userID string) (TOTPStatus, error) {
	var enabled int
	var codesJSON string
	var confirmed sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT enabled, recovery_codes, confirmed_at FROM user_totp WHERE user_id = ?`, userID).
		Scan(&enabled, &codesJSON, &confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTPStatus{}, nil
	}
	if err != nil {
		return TOTPStatus{}, err
	}
	st := TOTPStatus{Enabled: enabled != 0, Pending: enabled == 0}
	var codes []string
	_ = json.Unmarshal([]byte(codesJSON), &codes)
	st.RecoveryCodesLeft = len(codes)
	if confirmed.Valid {
		st.ConfirmedAt = confirmed.Time
	}
	return st, nil
}

func (s *SQLiteTOTPStore) Enabled(ctx context.Context, userID string) bool {
	st, err := s.Status(ctx, userID)
	return err == nil && st.Enabled
}

func (s *SQLiteTOTPStore) BeginEnrolment(ctx context.Context, userID, accountName string) (string, string, error) {
	if strings.TrimSpace(userID) == "" {
		return "", "", ErrUserNotFound
	}
	if s.Enabled(ctx, userID) {
		return "", "", ErrTOTPAlreadyEnabled
	}
	if accountName == "" {
		accountName = userID
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      TOTPIssuer,
		AccountName: accountName,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1, // the only algorithm every authenticator app supports
	})
	if err != nil {
		return "", "", err
	}
	enc, err := s.sealSecret(key.Secret())
	if err != nil {
		return "", "", err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_totp (user_id, secret_enc, enabled, recovery_codes, created_at, confirmed_at)
		VALUES (?, ?, 0, '[]', CURRENT_TIMESTAMP, NULL)
		ON CONFLICT(user_id) DO UPDATE SET secret_enc = excluded.secret_enc, enabled = 0, recovery_codes = '[]', created_at = CURRENT_TIMESTAMP, confirmed_at = NULL
	`, userID, enc)
	if err != nil {
		return "", "", err
	}
	return key.URL(), key.Secret(), nil
}

func (s *SQLiteTOTPStore) loadSecret(ctx context.Context, userID string) (secretB32 string, enabled bool, codes []string, err error) {
	var enc, codesJSON string
	var en int
	err = s.db.QueryRowContext(ctx, `SELECT secret_enc, enabled, recovery_codes FROM user_totp WHERE user_id = ?`, userID).
		Scan(&enc, &en, &codesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil, ErrTOTPNotEnabled
	}
	if err != nil {
		return "", false, nil, err
	}
	secretB32, err = s.openSecret(enc)
	if err != nil {
		return "", false, nil, fmt.Errorf("decrypt totp secret: %w", err)
	}
	_ = json.Unmarshal([]byte(codesJSON), &codes)
	return secretB32, en != 0, codes, nil
}

// validateCode allows one period of clock skew either side (RFC 6238 §5.2).
func validateCode(code, secretB32 string) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if code == "" {
		return false
	}
	ok, err := totp.ValidateCustom(code, secretB32, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

func (s *SQLiteTOTPStore) Confirm(ctx context.Context, userID, code string) ([]string, error) {
	secretB32, enabled, _, err := s.loadSecret(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrTOTPNotEnabled) {
			return nil, ErrTOTPNoPending
		}
		return nil, err
	}
	if enabled {
		return nil, ErrTOTPAlreadyEnabled
	}
	if !validateCode(code, secretB32) {
		return nil, ErrTOTPInvalidCode
	}
	plain, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashesJSON, _ := json.Marshal(hashes)
	res, err := s.db.ExecContext(ctx, `UPDATE user_totp SET enabled = 1, recovery_codes = ?, confirmed_at = CURRENT_TIMESTAMP WHERE user_id = ? AND enabled = 0`, string(hashesJSON), userID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrTOTPNoPending
	}
	return plain, nil
}

func (s *SQLiteTOTPStore) Verify(ctx context.Context, userID, code string) (bool, error) {
	secretB32, enabled, codes, err := s.loadSecret(ctx, userID)
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, ErrTOTPNotEnabled
	}
	if validateCode(code, secretB32) {
		return false, nil
	}
	// Recovery code: compare the digest in constant time, then consume it.
	h := hashRecoveryCode(code)
	idx := -1
	for i, c := range codes {
		if subtle.ConstantTimeCompare([]byte(c), []byte(h)) == 1 {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, ErrTOTPInvalidCode
	}
	remaining := append(append([]string{}, codes[:idx]...), codes[idx+1:]...)
	remJSON, _ := json.Marshal(remaining)
	// Guard the UPDATE with the old JSON so two concurrent uses of the
	// same recovery code cannot both succeed.
	oldJSON, _ := json.Marshal(codes)
	res, err := s.db.ExecContext(ctx, `UPDATE user_totp SET recovery_codes = ? WHERE user_id = ? AND recovery_codes = ?`, string(remJSON), userID, string(oldJSON))
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false, ErrTOTPInvalidCode
	}
	return true, nil
}

func (s *SQLiteTOTPStore) Disable(ctx context.Context, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTOTPNotEnabled
	}
	return nil
}

// newRecoveryCodes returns recoveryCodeCount plain codes ("xxxxx-xxxxx",
// lowercase base32 without ambiguous letters) and their SHA-256 digests.
func newRecoveryCodes() ([]string, []string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	plain := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		buf := make([]byte, 10)
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, err
		}
		b := make([]byte, 0, 11)
		for j, x := range buf {
			if j == 5 {
				b = append(b, '-')
			}
			b = append(b, alphabet[int(x)%len(alphabet)])
		}
		code := string(b)
		plain = append(plain, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return plain, hashes, nil
}

// hashRecoveryCode normalises (lowercase, no spaces/dashes) and digests a
// recovery code so a user may type it with or without the dash.
func hashRecoveryCode(code string) string {
	norm := strings.ToLower(strings.TrimSpace(code))
	norm = strings.ReplaceAll(norm, "-", "")
	norm = strings.ReplaceAll(norm, " ", "")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}
