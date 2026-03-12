package access

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestSQLiteAccessGroupStore_AddUserToGroup_QueryError covers return err when QueryRowContext fails (non-ErrNoRows).
func TestSQLiteAccessGroupStore_AddUserToGroup_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnError(wantErr)
	mock.ExpectRollback()

	err = store.AddUserToGroup(ctx, "u1", "g1")
	if err != wantErr {
		t.Fatalf("AddUserToGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_AddUserToGroup_ExecError covers return err when INSERT fails.
func TestSQLiteAccessGroupStore_AddUserToGroup_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("insert failed")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("INSERT OR IGNORE INTO user_groups").WithArgs("u1", "g1").WillReturnError(wantErr)
	mock.ExpectRollback()

	err = store.AddUserToGroup(ctx, "u1", "g1")
	if err != wantErr {
		t.Fatalf("AddUserToGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_AddUserToGroup_CommitError covers return err when Commit fails.
func TestSQLiteAccessGroupStore_AddUserToGroup_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("commit failed")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("INSERT OR IGNORE INTO user_groups").WithArgs("u1", "g1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(wantErr)

	err = store.AddUserToGroup(ctx, "u1", "g1")
	if err != wantErr {
		t.Fatalf("AddUserToGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_AddUserToGroup_BeginError covers return err when BeginTx fails.
func TestSQLiteAccessGroupStore_AddUserToGroup_BeginError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("begin failed")
	mock.ExpectBegin().WillReturnError(wantErr)

	err = store.AddUserToGroup(ctx, "u1", "g1")
	if err != wantErr {
		t.Fatalf("AddUserToGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_RemoveUserFromGroup_QueryError covers return err when QueryRow fails (non-ErrNoRows).
func TestSQLiteAccessGroupStore_RemoveUserFromGroup_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnError(wantErr)

	err = store.RemoveUserFromGroup(ctx, "u1", "g1")
	if err != wantErr {
		t.Fatalf("RemoveUserFromGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_RemoveUserFromGroup_ExecError covers return err when DELETE fails.
func TestSQLiteAccessGroupStore_RemoveUserFromGroup_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("delete failed")
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("DELETE FROM user_groups").WithArgs("u1", "g1").WillReturnError(wantErr)

	err = store.RemoveUserFromGroup(ctx, "u1", "g1")
	if err != wantErr {
		t.Fatalf("RemoveUserFromGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_UserIDsForGroup_QueryError covers UserIDsForGroup when QueryContext fails.
func TestSQLiteAccessGroupStore_UserIDsForGroup_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT user_id").WithArgs("g1", 10000, 0).WillReturnError(wantErr)

	_, err = store.UserIDsForGroup(ctx, "g1", nil)
	if err != wantErr {
		t.Fatalf("UserIDsForGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_UserIDsForGroup_ScanErr covers rows.Scan error (NULL user_id).
func TestSQLiteAccessGroupStore_UserIDsForGroup_ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"user_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT user_id").WithArgs("g1", 10000, 0).WillReturnRows(rows)

	_, err = store.UserIDsForGroup(ctx, "g1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL user_id")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_UserIDsForGroup_RowsErr covers rows.Err() path.
func TestSQLiteAccessGroupStore_UserIDsForGroup_RowsErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("rows error")
	rows := sqlmock.NewRows([]string{"user_id"}).AddRow("u1").RowError(0, wantErr)
	mock.ExpectQuery("SELECT user_id").WithArgs("g1", 10000, 0).WillReturnRows(rows)

	_, err = store.UserIDsForGroup(ctx, "g1", nil)
	if err != wantErr {
		t.Fatalf("UserIDsForGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TagsForGroup_QueryError_Mock covers TagsForGroup when query fails (mock).
func TestSQLiteAccessGroupStore_TagsForGroup_QueryError_Mock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT tag FROM group_tags").WithArgs("g1").WillReturnError(wantErr)

	_, err = store.TagsForGroup(ctx, "g1")
	if err != wantErr {
		t.Fatalf("TagsForGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TagsForGroup_ScanErr covers rows.Scan error in TagsForGroup (NULL tag).
func TestSQLiteAccessGroupStore_TagsForGroup_ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"tag"}).AddRow(nil)
	mock.ExpectQuery("SELECT tag FROM group_tags").WithArgs("g1").WillReturnRows(rows)

	_, err = store.TagsForGroup(ctx, "g1")
	if err == nil {
		t.Fatal("expected Scan error for NULL tag")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TagsForGroup_RowsErr covers rows.Err() in TagsForGroup.
func TestSQLiteAccessGroupStore_TagsForGroup_RowsErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("rows err")
	rows := sqlmock.NewRows([]string{"tag"}).AddRow("a").RowError(0, wantErr)
	mock.ExpectQuery("SELECT tag FROM group_tags").WithArgs("g1").WillReturnRows(rows)

	_, err = store.TagsForGroup(ctx, "g1")
	if err != wantErr {
		t.Fatalf("TagsForGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_SetGroupTags_QueryRowError_Mock covers SetGroupTags when SELECT fails (mock).
func TestSQLiteAccessGroupStore_SetGroupTags_QueryRowError_Mock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("select failed")
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnError(wantErr)

	err = store.SetGroupTags(ctx, "g1", []string{"a"})
	if err != wantErr {
		t.Fatalf("SetGroupTags: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_SetGroupTags_DeleteError covers SetGroupTags when DELETE fails.
func TestSQLiteAccessGroupStore_SetGroupTags_DeleteError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("delete failed")
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("DELETE FROM group_tags").WithArgs("g1").WillReturnError(wantErr)

	err = store.SetGroupTags(ctx, "g1", []string{"a"})
	if err != wantErr {
		t.Fatalf("SetGroupTags: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_SetGroupTags_InsertError covers SetGroupTags when INSERT fails.
func TestSQLiteAccessGroupStore_SetGroupTags_InsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("insert failed")
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("DELETE FROM group_tags").WithArgs("g1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO group_tags").WithArgs("g1", "a").WillReturnError(wantErr)

	err = store.SetGroupTags(ctx, "g1", []string{"a"})
	if err != wantErr {
		t.Fatalf("SetGroupTags: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_AddTargetToGroup_QueryError covers AddTargetToGroup when QueryRow fails.
func TestSQLiteAccessGroupStore_AddTargetToGroup_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnError(wantErr)
	mock.ExpectRollback()

	err = store.AddTargetToGroup(ctx, "g1", "t1")
	if err != wantErr {
		t.Fatalf("AddTargetToGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_AddTargetToGroup_ExecError covers AddTargetToGroup when INSERT fails.
func TestSQLiteAccessGroupStore_AddTargetToGroup_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("insert failed")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("INSERT OR IGNORE INTO group_targets").WithArgs("g1", "t1").WillReturnError(wantErr)
	mock.ExpectRollback()

	err = store.AddTargetToGroup(ctx, "g1", "t1")
	if err != wantErr {
		t.Fatalf("AddTargetToGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_AddTargetToGroup_CommitError covers AddTargetToGroup when Commit fails.
func TestSQLiteAccessGroupStore_AddTargetToGroup_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("commit failed")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1 FROM access_groups WHERE id = \\?").WithArgs("g1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("INSERT OR IGNORE INTO group_targets").WithArgs("g1", "t1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(wantErr)

	err = store.AddTargetToGroup(ctx, "g1", "t1")
	if err != wantErr {
		t.Fatalf("AddTargetToGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_Get_QueryError covers Get when QueryRow returns non-ErrNoRows error.
func TestSQLiteAccessGroupStore_Get_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT id, name").WithArgs("g1").WillReturnError(wantErr)

	_, err = store.Get(ctx, "g1")
	if err != wantErr {
		t.Fatalf("Get: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_Get_ErrNoRows covers Get returning ErrGroupNotFound.
func TestSQLiteAccessGroupStore_Get_ErrNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	mock.ExpectQuery("SELECT id, name").WithArgs("missing").WillReturnError(sql.ErrNoRows)

	_, err = store.Get(ctx, "missing")
	if err != ErrGroupNotFound {
		t.Fatalf("Get: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Query1Error covers GroupIDsForUser when first query fails.
func TestSQLiteAccessGroupStore_GroupIDsForUser_Query1Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnError(wantErr)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("GroupIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Rows1Err covers rows1.Err() path.
func TestSQLiteAccessGroupStore_GroupIDsForUser_Rows1Err(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("rows1 err")
	rows := sqlmock.NewRows([]string{"group_id"}).AddRow("g1").RowError(0, wantErr)
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("GroupIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Query2Error covers GroupIDsForUser when second query fails.
func TestSQLiteAccessGroupStore_GroupIDsForUser_Query2Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query2 failed")
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gt.group_id").WithArgs("u1").WillReturnError(wantErr)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("GroupIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Query3Error covers GroupIDsForUser when third query fails.
func TestSQLiteAccessGroupStore_GroupIDsForUser_Query3Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query3 failed")
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gt.group_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gtag.group_id").WithArgs("u1").WillReturnError(wantErr)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("GroupIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Rows1ScanErr covers rows1.Scan error (NULL group_id).
func TestSQLiteAccessGroupStore_GroupIDsForUser_Rows1ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"group_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL group_id in rows1")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Rows2Err covers rows2.Err() in GroupIDsForUser.
func TestSQLiteAccessGroupStore_GroupIDsForUser_Rows2Err(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("rows2 err")
	rows := sqlmock.NewRows([]string{"group_id"}).AddRow("g1").RowError(0, wantErr)
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gt.group_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("GroupIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Rows3Err covers rows3.Err() in GroupIDsForUser.
func TestSQLiteAccessGroupStore_GroupIDsForUser_Rows3Err(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("rows3 err")
	rows := sqlmock.NewRows([]string{"group_id"}).AddRow("g1").RowError(0, wantErr)
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gt.group_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gtag.group_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("GroupIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Rows2ScanErr covers rows2.Scan error (NULL group_id).
func TestSQLiteAccessGroupStore_GroupIDsForUser_Rows2ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"group_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gt.group_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL group_id")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_Rows3ScanErr covers rows3.Scan error (NULL group_id).
func TestSQLiteAccessGroupStore_GroupIDsForUser_Rows3ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"group_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT group_id FROM user_groups WHERE user_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gt.group_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("SELECT DISTINCT gtag.group_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.GroupIDsForUser(ctx, "u1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL group_id in rows3")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForGroup_QueryError covers TargetIDsForGroup when query fails.
func TestSQLiteAccessGroupStore_TargetIDsForGroup_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT target_id").WithArgs("g1", 10000, 0).WillReturnError(wantErr)

	_, err = store.TargetIDsForGroup(ctx, "g1", nil)
	if err != wantErr {
		t.Fatalf("TargetIDsForGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForGroup_ScanErr covers rows.Scan error in TargetIDsForGroup.
func TestSQLiteAccessGroupStore_TargetIDsForGroup_ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"target_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT target_id").WithArgs("g1", 10000, 0).WillReturnRows(rows)

	_, err = store.TargetIDsForGroup(ctx, "g1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL target_id")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForGroup_RowsErr covers rows.Err() in TargetIDsForGroup.
func TestSQLiteAccessGroupStore_TargetIDsForGroup_RowsErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("rows err")
	rows := sqlmock.NewRows([]string{"target_id"}).AddRow("t1").RowError(0, wantErr)
	mock.ExpectQuery("SELECT target_id").WithArgs("g1", 10000, 0).WillReturnRows(rows)

	_, err = store.TargetIDsForGroup(ctx, "g1", nil)
	if err != wantErr {
		t.Fatalf("TargetIDsForGroup: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_Rows1Err covers rows1.Err() in TargetIDsForUser.
func TestSQLiteAccessGroupStore_TargetIDsForUser_Rows1Err(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("rows1 err")
	rows := sqlmock.NewRows([]string{"target_id"}).AddRow("t1").RowError(0, wantErr)
	mock.ExpectQuery("SELECT DISTINCT gt.target_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.TargetIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("TargetIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_Rows1ScanErr covers rows1.Scan error (NULL target_id).
func TestSQLiteAccessGroupStore_TargetIDsForUser_Rows1ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"target_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT DISTINCT gt.target_id").WithArgs("u1").WillReturnRows(rows)

	_, err = store.TargetIDsForUser(ctx, "u1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL target_id in rows1")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_Query1Error covers TargetIDsForUser when first query fails.
func TestSQLiteAccessGroupStore_TargetIDsForUser_Query1Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT DISTINCT gt.target_id").WithArgs("u1").WillReturnError(wantErr)

	_, err = store.TargetIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("TargetIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_Query2Error covers TargetIDsForUser when second query fails.
func TestSQLiteAccessGroupStore_TargetIDsForUser_Query2Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query2 failed")
	mock.ExpectQuery("SELECT DISTINCT gt.target_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"target_id"}))
	mock.ExpectQuery("target_tags tt").WithArgs("u1").WillReturnError(wantErr)

	_, err = store.TargetIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("TargetIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_Query3Error covers TargetIDsForUser when third query fails.
func TestSQLiteAccessGroupStore_TargetIDsForUser_Query3Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	wantErr := errors.New("query3 failed")
	mock.ExpectQuery("SELECT DISTINCT gt.target_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"target_id"}))
	mock.ExpectQuery("target_tags tt").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"target_id"}))
	mock.ExpectQuery("group_tags gtag").WithArgs("u1").WillReturnError(wantErr)

	_, err = store.TargetIDsForUser(ctx, "u1", nil)
	if err != wantErr {
		t.Fatalf("TargetIDsForUser: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_Rows2ScanErr covers rows2.Scan error (NULL target_id).
func TestSQLiteAccessGroupStore_TargetIDsForUser_Rows2ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"target_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT DISTINCT gt.target_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"target_id"}))
	mock.ExpectQuery("target_tags tt").WithArgs("u1").WillReturnRows(rows)

	_, err = store.TargetIDsForUser(ctx, "u1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL target_id")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_Rows3ScanErr covers rows3.Scan error (NULL target_id).
func TestSQLiteAccessGroupStore_TargetIDsForUser_Rows3ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"target_id"}).AddRow(nil)
	mock.ExpectQuery("SELECT DISTINCT gt.target_id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"target_id"}))
	mock.ExpectQuery("target_tags tt").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"target_id"}))
	mock.ExpectQuery("group_tags gtag").WithArgs("u1").WillReturnRows(rows)

	_, err = store.TargetIDsForUser(ctx, "u1", nil)
	if err == nil {
		t.Fatal("expected Scan error for NULL target_id in rows3")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_Get_QueryError covers Get when QueryRow returns non-ErrNoRows error.
func TestSQLiteTargetStore_Get_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT id, name, host, port, protocol, path").WithArgs("t1").WillReturnError(wantErr)

	_, err = store.Get(ctx, "t1")
	if err != wantErr {
		t.Fatalf("Get: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_Get_ErrNoRows covers Get returning ErrTargetNotFound.
func TestSQLiteTargetStore_Get_ErrNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	mock.ExpectQuery("SELECT id, name, host, port, protocol, path").WithArgs("missing").WillReturnError(sql.ErrNoRows)

	_, err = store.Get(ctx, "missing")
	if err != ErrTargetNotFound {
		t.Fatalf("Get: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_Delete_ExecError covers Delete when ExecContext fails.
func TestSQLiteTargetStore_Delete_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("delete failed")
	mock.ExpectExec("DELETE FROM targets WHERE id").WithArgs("t1").WillReturnError(wantErr)

	err = store.Delete(ctx, "t1")
	if err != wantErr {
		t.Fatalf("Delete: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_Update_ExecError covers Update when ExecContext fails.
func TestSQLiteTargetStore_Update_ExecError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("update failed")
	mock.ExpectExec("UPDATE targets SET").
		WithArgs("n", "h", 22, "ssh", "", "", "", "", "", 1, 0, 0, "t1").
		WillReturnError(wantErr)

	_, err = store.Update(ctx, "t1", "n", "h", 22, ProtocolSSH, "", "", "", "", "", true, false, false)
	if err != wantErr {
		t.Fatalf("Update: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_Update_RowsAffectedZero covers Update when no row is updated (ErrTargetNotFound).
func TestSQLiteTargetStore_Update_RowsAffectedZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	mock.ExpectExec("UPDATE targets SET").
		WithArgs("n", "h", 22, "ssh", "", "", "", "", "", 1, 0, 0, "missing").
		WillReturnResult(sqlmock.NewResult(0, 0))

	_, err = store.Update(ctx, "missing", "n", "h", 22, ProtocolSSH, "", "", "", "", "", true, false, false)
	if err != ErrTargetNotFound {
		t.Fatalf("Update: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_Update_Success covers Update success path (Exec then Get).
func TestSQLiteTargetStore_Update_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	mock.ExpectExec("UPDATE targets SET").
		WithArgs("new", "10.0.0.2", 2222, "telnet", "path", "user", "", "", "", 1, 0, 0, "t1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, name, host, port, protocol, path").
		WithArgs("t1").
		WillReturnRows(
			sqlmock.NewRows([]string{"id", "name", "host", "port", "protocol", "path", "ssh_username", "ssh_password", "ssh_private_key", "ssh_private_key_passphrase", "sftp_enabled", "ftp_enabled", "tftp_enabled"}).
				AddRow("t1", "new", "10.0.0.2", 2222, "telnet", "path", "user", "", "", "", 1, 0, 0))

	got, err := store.Update(ctx, "t1", "new", "10.0.0.2", 2222, ProtocolTelnet, "path", "user", "", "", "", true, false, false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Name != "new" || got.Host != "10.0.0.2" || got.Port != 2222 || got.Protocol != ProtocolTelnet || got.Path != "path" || got.SSHUsername != "user" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_TagsForTarget_QueryError_Mock covers TagsForTarget when query fails (mock).
func TestSQLiteTargetStore_TagsForTarget_QueryError_Mock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT tag FROM target_tags").WithArgs("t1").WillReturnError(wantErr)

	_, err = store.TagsForTarget(ctx, "t1")
	if err != wantErr {
		t.Fatalf("TagsForTarget: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_TagsForTarget_ScanErr covers rows.Scan error in TagsForTarget.
func TestSQLiteTargetStore_TagsForTarget_ScanErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"tag"}).AddRow(nil)
	mock.ExpectQuery("SELECT tag FROM target_tags").WithArgs("t1").WillReturnRows(rows)

	_, err = store.TagsForTarget(ctx, "t1")
	if err == nil {
		t.Fatal("expected Scan error for NULL tag")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_TagsForTarget_RowsErr covers rows.Err() in TagsForTarget.
func TestSQLiteTargetStore_TagsForTarget_RowsErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("rows err")
	rows := sqlmock.NewRows([]string{"tag"}).AddRow("a").RowError(0, wantErr)
	mock.ExpectQuery("SELECT tag FROM target_tags").WithArgs("t1").WillReturnRows(rows)

	_, err = store.TagsForTarget(ctx, "t1")
	if err != wantErr {
		t.Fatalf("TagsForTarget: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_SetTargetTags_QueryRowError_Mock covers SetTargetTags when SELECT fails (mock).
func TestSQLiteTargetStore_SetTargetTags_QueryRowError_Mock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("select failed")
	mock.ExpectQuery("SELECT 1 FROM targets WHERE id = \\?").WithArgs("t1").WillReturnError(wantErr)

	err = store.SetTargetTags(ctx, "t1", []string{"a"})
	if err != wantErr {
		t.Fatalf("SetTargetTags: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_SetTargetTags_DeleteError covers SetTargetTags when DELETE fails.
func TestSQLiteTargetStore_SetTargetTags_DeleteError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("delete failed")
	mock.ExpectQuery("SELECT 1 FROM targets WHERE id = \\?").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("DELETE FROM target_tags").WithArgs("t1").WillReturnError(wantErr)

	err = store.SetTargetTags(ctx, "t1", []string{"a"})
	if err != wantErr {
		t.Fatalf("SetTargetTags: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_SetTargetTags_InsertError covers SetTargetTags when INSERT fails.
func TestSQLiteTargetStore_SetTargetTags_InsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("insert failed")
	mock.ExpectQuery("SELECT 1 FROM targets WHERE id = \\?").WithArgs("t1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mock.ExpectExec("DELETE FROM target_tags").WithArgs("t1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO target_tags").WithArgs("t1", "a").WillReturnError(wantErr)

	err = store.SetTargetTags(ctx, "t1", []string{"a"})
	if err != wantErr {
		t.Fatalf("SetTargetTags: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_ListByIDs_QueryError covers ListByIDs when chunk query fails.
func TestSQLiteTargetStore_ListByIDs_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("query failed")
	mock.ExpectQuery("SELECT id, name, host, port, protocol, path").WithArgs("t1").WillReturnError(wantErr)

	_, err = store.ListByIDs(ctx, []TargetID{"t1"}, nil)
	if err != wantErr {
		t.Fatalf("ListByIDs: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// TestSQLiteTargetStore_ListByIDs_RowsErr covers rows.Err() in ListByIDs chunk loop.
func TestSQLiteTargetStore_ListByIDs_RowsErr(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()

	wantErr := errors.New("rows err")
	row := sqlmock.NewRows([]string{"id", "name", "host", "port", "protocol", "path", "ssh_username", "ssh_password"}).
		AddRow("t1", "n", "h", 22, "ssh", "", "", "").RowError(0, wantErr)
	mock.ExpectQuery("SELECT id, name, host, port, protocol, path").WithArgs("t1").WillReturnRows(row)

	_, err = store.ListByIDs(ctx, []TargetID{"t1"}, nil)
	if err != wantErr {
		t.Fatalf("ListByIDs: got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
