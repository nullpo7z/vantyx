package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestTOTPStore(t *testing.T) *SQLiteTOTPStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "totp.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := NewSQLiteUserStore(db).CreateUser("user1", "user1", "Password1!", ""); err != nil && !errors.Is(err, ErrUserExists) {
		t.Fatalf("create user: %v", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return NewSQLiteTOTPStore(db, key)
}

func codeFor(t *testing.T, secretB32 string) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secretB32, time.Now().UTC(), totp.ValidateOpts{
		Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

func TestTOTPStore_EnrolConfirmVerify(t *testing.T) {
	s := newTestTOTPStore(t)
	ctx := context.Background()

	st, err := s.Status(ctx, "user1")
	if err != nil || st.Enabled || st.Pending {
		t.Fatalf("fresh status = %+v, %v; want disabled/not pending", st, err)
	}
	if _, err := s.Verify(ctx, "user1", "000000"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("Verify before enrolment: err=%v want ErrTOTPNotEnabled", err)
	}

	otpauthURL, secretB32, err := s.BeginEnrolment(ctx, "user1", "user1")
	if err != nil {
		t.Fatalf("BeginEnrolment: %v", err)
	}
	if !strings.HasPrefix(otpauthURL, "otpauth://totp/") || !strings.Contains(otpauthURL, "issuer="+TOTPIssuer) {
		t.Fatalf("unexpected otpauth URL %q", otpauthURL)
	}
	st, _ = s.Status(ctx, "user1")
	if st.Enabled || !st.Pending {
		t.Fatalf("status after BeginEnrolment = %+v; want pending", st)
	}
	// Pending enrolment must not be enforced on login.
	if s.Enabled(ctx, "user1") {
		t.Fatal("Enabled() reported true while enrolment is only pending")
	}
	if _, err := s.Verify(ctx, "user1", codeFor(t, secretB32)); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("Verify while pending: err=%v want ErrTOTPNotEnabled", err)
	}

	if _, err := s.Confirm(ctx, "user1", "123456"); !errors.Is(err, ErrTOTPInvalidCode) {
		t.Fatalf("Confirm with bogus code: err=%v want ErrTOTPInvalidCode", err)
	}
	codes, err := s.Confirm(ctx, "user1", codeFor(t, secretB32))
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), recoveryCodeCount)
	}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Fatalf("recovery code %q not in xxxxx-xxxxx form", c)
		}
	}
	st, _ = s.Status(ctx, "user1")
	if !st.Enabled || st.Pending || st.RecoveryCodesLeft != recoveryCodeCount || st.ConfirmedAt.IsZero() {
		t.Fatalf("status after Confirm = %+v", st)
	}
	if !s.Enabled(ctx, "user1") {
		t.Fatal("Enabled() false after Confirm")
	}

	// Second confirm / re-enrol while enabled is refused.
	if _, err := s.Confirm(ctx, "user1", codeFor(t, secretB32)); !errors.Is(err, ErrTOTPAlreadyEnabled) {
		t.Fatalf("Confirm twice: err=%v want ErrTOTPAlreadyEnabled", err)
	}
	if _, _, err := s.BeginEnrolment(ctx, "user1", "user1"); !errors.Is(err, ErrTOTPAlreadyEnabled) {
		t.Fatalf("BeginEnrolment while enabled: err=%v want ErrTOTPAlreadyEnabled", err)
	}

	// A live TOTP code verifies (and is not a recovery code).
	used, err := s.Verify(ctx, "user1", codeFor(t, secretB32))
	if err != nil || used {
		t.Fatalf("Verify(totp) = used=%v err=%v", used, err)
	}
	// Whitespace inside the code is tolerated (apps show "123 456").
	c := codeFor(t, secretB32)
	if _, err := s.Verify(ctx, "user1", c[:3]+" "+c[3:]); err != nil {
		t.Fatalf("Verify with space: %v", err)
	}
	if _, err := s.Verify(ctx, "user1", "000000"); !errors.Is(err, ErrTOTPInvalidCode) {
		t.Fatalf("Verify wrong code: err=%v want ErrTOTPInvalidCode", err)
	}
	if _, err := s.Verify(ctx, "user1", ""); !errors.Is(err, ErrTOTPInvalidCode) {
		t.Fatalf("Verify empty code: err=%v want ErrTOTPInvalidCode", err)
	}
}

func TestTOTPStore_RecoveryCodesAreSingleUse(t *testing.T) {
	s := newTestTOTPStore(t)
	ctx := context.Background()
	_, secretB32, err := s.BeginEnrolment(ctx, "user1", "user1")
	if err != nil {
		t.Fatalf("BeginEnrolment: %v", err)
	}
	codes, err := s.Confirm(ctx, "user1", codeFor(t, secretB32))
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Dash and case are normalised.
	upper := strings.ToUpper(strings.ReplaceAll(codes[0], "-", ""))
	used, err := s.Verify(ctx, "user1", upper)
	if err != nil || !used {
		t.Fatalf("Verify(recovery) = used=%v err=%v", used, err)
	}
	if _, err := s.Verify(ctx, "user1", codes[0]); !errors.Is(err, ErrTOTPInvalidCode) {
		t.Fatalf("recovery code reused: err=%v want ErrTOTPInvalidCode", err)
	}
	st, _ := s.Status(ctx, "user1")
	if st.RecoveryCodesLeft != recoveryCodeCount-1 {
		t.Fatalf("RecoveryCodesLeft = %d, want %d", st.RecoveryCodesLeft, recoveryCodeCount-1)
	}
	// The remaining codes still work, one by one.
	for _, c := range codes[1:] {
		if used, err := s.Verify(ctx, "user1", c); err != nil || !used {
			t.Fatalf("Verify(%q) = used=%v err=%v", c, used, err)
		}
	}
	st, _ = s.Status(ctx, "user1")
	if st.RecoveryCodesLeft != 0 {
		t.Fatalf("RecoveryCodesLeft = %d, want 0", st.RecoveryCodesLeft)
	}
	// TOTP itself keeps working after the recovery codes are exhausted.
	if _, err := s.Verify(ctx, "user1", codeFor(t, secretB32)); err != nil {
		t.Fatalf("Verify(totp) after exhausting recovery codes: %v", err)
	}
}

func TestTOTPStore_DisableAndReenrol(t *testing.T) {
	s := newTestTOTPStore(t)
	ctx := context.Background()

	if err := s.Disable(ctx, "user1"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("Disable without factor: err=%v want ErrTOTPNotEnabled", err)
	}
	_, first, err := s.BeginEnrolment(ctx, "user1", "user1")
	if err != nil {
		t.Fatalf("BeginEnrolment: %v", err)
	}
	if _, err := s.Confirm(ctx, "user1", codeFor(t, first)); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := s.Disable(ctx, "user1"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if s.Enabled(ctx, "user1") {
		t.Fatal("still enabled after Disable")
	}
	if _, err := s.Verify(ctx, "user1", codeFor(t, first)); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("Verify after Disable: err=%v want ErrTOTPNotEnabled", err)
	}
	// Re-enrolment issues a fresh secret; the old one no longer verifies.
	_, second, err := s.BeginEnrolment(ctx, "user1", "user1")
	if err != nil {
		t.Fatalf("BeginEnrolment (again): %v", err)
	}
	if first == second {
		t.Fatal("re-enrolment reused the previous secret")
	}
	if _, err := s.Confirm(ctx, "user1", codeFor(t, first)); !errors.Is(err, ErrTOTPInvalidCode) {
		t.Fatalf("Confirm with old secret: err=%v want ErrTOTPInvalidCode", err)
	}
	if _, err := s.Confirm(ctx, "user1", codeFor(t, second)); err != nil {
		t.Fatalf("Confirm with new secret: %v", err)
	}
}

func TestTOTPStore_SecretIsEncryptedAtRest(t *testing.T) {
	s := newTestTOTPStore(t)
	ctx := context.Background()
	_, secretB32, err := s.BeginEnrolment(ctx, "user1", "user1")
	if err != nil {
		t.Fatalf("BeginEnrolment: %v", err)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT secret_enc FROM user_totp WHERE user_id = 'user1'`).Scan(&stored); err != nil {
		t.Fatalf("select: %v", err)
	}
	if stored == secretB32 || strings.Contains(stored, secretB32) {
		t.Fatal("TOTP secret stored in plaintext")
	}
}

func TestTOTPStore_DeletingUserCascades(t *testing.T) {
	s := newTestTOTPStore(t)
	ctx := context.Background()
	if _, _, err := s.BeginEnrolment(ctx, "user1", "user1"); err != nil {
		t.Fatalf("BeginEnrolment: %v", err)
	}
	if err := NewSQLiteUserStore(s.db).DeleteUser("user1"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	st, err := s.Status(ctx, "user1")
	if err != nil || st.Pending || st.Enabled {
		t.Fatalf("status after user delete = %+v, %v; want empty", st, err)
	}
}
