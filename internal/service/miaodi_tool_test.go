package service

import (
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"miaodi-agent/internal/cache"
	"miaodi-agent/internal/model"
	"miaodi-agent/internal/repository"
)

type fakeMiaodi struct {
	checkResult     bool
	putResult       map[string]interface{}
	sendEmailResult map[string]interface{}
	sendEmailErr    error
	getKeyResult    map[string]interface{}
	getKeyErr       error
}

func (f *fakeMiaodi) Check(key string) bool { return f.checkResult }
func (f *fakeMiaodi) GetInfo(key string) (map[string]interface{}, error) {
	return map[string]interface{}{"code": 20000}, nil
}
func (f *fakeMiaodi) SendEmail(email string) (map[string]interface{}, error) {
	if f.sendEmailResult != nil || f.sendEmailErr != nil {
		return f.sendEmailResult, f.sendEmailErr
	}
	return map[string]interface{}{"code": 20000}, nil
}
func (f *fakeMiaodi) GetKey(email, code string) (map[string]interface{}, error) {
	if f.getKeyResult != nil || f.getKeyErr != nil {
		return f.getKeyResult, f.getKeyErr
	}
	return map[string]interface{}{"code": 20000, "key": "key-from-email"}, nil
}
func (f *fakeMiaodi) PutText(key, book, chapter, title, content string) (map[string]interface{}, error) {
	return f.putResult, nil
}

func newToolExecutorMock(t *testing.T) (*ToolExecutor, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	userRepo := repository.NewUserRepo(db)
	convRepo := repository.NewConversationRepo(db)
	pendingRepo := repository.NewPendingImageRepo(db)
	logRepo := repository.NewCallLogRepo(db)
	miaodi := &fakeMiaodi{checkResult: true, putResult: map[string]interface{}{"code": 20000}}
	return NewToolExecutor(miaodi, userRepo, convRepo, pendingRepo, logRepo, cache.NopCache{}, nopPersistQueue{}), mock
}

func TestToolExecutor_bindMiaodiKey_Success(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("UPDATE agent_users SET apikey").WithArgs("key1", "bound", "u1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "key1", "miaodi", "bind_key", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Status: "unbound"}
	res := exec.Execute(user, "u1", 1, "bind_miaodi_key", `{"key":"key1"}`)
	if res != "绑定成功，你现在可以保存笔记和图片了" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_bindMiaodiKey_CheckFail(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	exec.miaodi = &fakeMiaodi{checkResult: false}
	user := &model.User{ChannelUserID: "u1"}
	res := exec.Execute(user, "u1", 1, "bind_miaodi_key", `{"key":"bad"}`)
	if res != "Key 校验失败，请检查是否正确" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_sendMiaodiEmailCode_Success(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("UPDATE agent_users SET email").WithArgs("u@example.com", userStatusWaitingEmailCode, "u1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "", "miaodi", "send_email", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Status: userStatusUnbound}
	res := exec.Execute(user, "u1", 1, "send_miaodi_email_code", `{"email":"u@example.com"}`)
	if res != "邮件已发送，请检查收件箱并回复收到的验证码" {
		t.Errorf("unexpected result: %s", res)
	}
	if user.Email != "u@example.com" || user.Status != userStatusWaitingEmailCode {
		t.Errorf("unexpected user state: %+v", user)
	}
}

func TestToolExecutor_sendMiaodiEmailCode_InvalidEmail(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "send_miaodi_email_code", `{"email":"bad"}`)
	if res != "邮箱格式不正确，请检查后重试" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_bindMiaodiByEmailCode_Success(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("UPDATE agent_users SET apikey").WithArgs("key-from-email", userStatusBound, "u1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "key-from-email", "miaodi", "get_key", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Email: "u@example.com", Status: userStatusWaitingEmailCode}
	res := exec.Execute(user, "u1", 1, "bind_miaodi_by_email_code", `{"code":"123456"}`)
	if res != "绑定成功，你现在可以保存笔记和图片了" {
		t.Errorf("unexpected result: %s", res)
	}
	if user.APIKey != "key-from-email" || user.Status != userStatusBound {
		t.Errorf("unexpected user state: %+v", user)
	}
}

func TestToolExecutor_bindMiaodiByEmailCode_MissingEmail(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 1, "bind_miaodi_by_email_code", `{"code":"123456"}`)
	if res != "请先提供邮箱获取验证码" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getMiaodiKey(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{Status: userStatusBound, APIKey: "key1"}
	res := exec.Execute(user, "u1", 1, "get_miaodi_key", `{}`)
	if res != "key1" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getMiaodiAnnualReport(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "key1", "miaodi", "annual_report", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Status: userStatusBound, APIKey: "key1"}
	res := exec.Execute(user, "u1", 1, "get_miaodi_annual_report", `{}`)
	if !strings.Contains(res, "https://api.libv.cc/miaodi/report/page?key=key1") {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_unbindMiaodiKey(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("UPDATE agent_users SET apikey").WithArgs("u1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "", "miaodi", "unbind_key", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Status: userStatusBound, APIKey: "key1", Email: "u@example.com"}
	res := exec.Execute(user, "u1", 1, "unbind_miaodi_key", `{}`)
	if res != "解绑成功，欢迎再次使用" {
		t.Errorf("unexpected result: %s", res)
	}
	if user.APIKey != "" || user.Email != "" || user.Status != userStatusUnbound {
		t.Errorf("unexpected user state: %+v", user)
	}
}

func TestToolExecutor_setSavePath(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("UPDATE agent_users SET book").WithArgs("b", "c", "t", "u1").WillReturnResult(sqlmock.NewResult(0, 1))

	user := &model.User{ChannelUserID: "u1"}
	res := exec.Execute(user, "u1", 1, "set_save_path", `{"book":"b","chapter":"c","title":"t"}`)
	if res == "" || res == "参数解析失败" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_getUserProfile(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "abcd1234xyz", Book: "b", Chara: "c"}
	res := exec.Execute(user, "u1", 1, "get_user_profile", `{}`)
	if res == "" {
		t.Error("expected profile content")
	}
}

func TestToolExecutor_saveTextNote_Success(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "key1", "miaodi", "put_text", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "key1", Book: "b", Chara: "c"}
	res := exec.Execute(user, "u1", 1, "save_text_note", `{"content":"hello"}`)
	if res == "" || res == "保存失败" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_saveTextNote_FailCode(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	exec.miaodi = &fakeMiaodi{checkResult: true, putResult: map[string]interface{}{"code": 50000, "message": "err"}}
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "key1", "miaodi", "put_text_failed", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "key1", Book: "b", Chara: "c"}
	res := exec.Execute(user, "u1", 1, "save_text_note", `{"content":"hello"}`)
	if res == "" {
		t.Error("expected error result")
	}
}

func TestToolExecutor_saveTextNote_NotBound(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{ChannelUserID: "u1", Status: "unbound"}
	res := exec.Execute(user, "u1", 1, "save_text_note", `{"content":"hello"}`)
	if res != "尚未绑定喵滴 Key，请先绑定" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_saveImageNote(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("INSERT INTO pending_images").WithArgs("key1", "http://img", "b", "c", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO api_call_log").WithArgs("u1", "key1", "miaodi", "save_image_pending", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "key1", Book: "b", Chara: "c"}
	res := exec.Execute(user, "u1", 1, "save_image_note", `{"image_url":"http://img"}`)
	if res == "" {
		t.Error("expected result")
	}
}

func TestToolExecutor_UnknownTool(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{}
	res := exec.Execute(user, "u1", 1, "unknown", `{}`)
	if res != "未知工具: unknown" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_bindMiaodiKey_InvalidArgs(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{}
	if res := exec.Execute(user, "u1", 1, "bind_miaodi_key", `invalid`); res == "" {
		t.Error("expected error result")
	}
	if res := exec.Execute(user, "u1", 1, "bind_miaodi_key", `{"key":""}`); res == "" {
		t.Error("expected empty key error")
	}
}

func TestToolExecutor_setSavePath_InvalidArgs(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{}
	if res := exec.Execute(user, "u1", 1, "set_save_path", `invalid`); res == "" {
		t.Error("expected error result")
	}
	if res := exec.Execute(user, "u1", 1, "set_save_path", `{"book":"","chapter":""}`); res == "" {
		t.Error("expected empty error")
	}
}

func TestToolExecutor_saveTextNote_InvalidArgs(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "key1"}
	if res := exec.Execute(user, "u1", 1, "save_text_note", `invalid`); res == "" {
		t.Error("expected error result")
	}
	if res := exec.Execute(user, "u1", 1, "save_text_note", `{"content":""}`); res == "" {
		t.Error("expected empty content error")
	}
}

func TestToolExecutor_saveTextNote_PutError(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	exec.miaodi = &fakeMiaodi{checkResult: true, putResult: nil} // putResult nil triggers err path
	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "key1", Book: "b", Chara: "c"}
	if res := exec.Execute(user, "u1", 1, "save_text_note", `{"content":"hello"}`); res == "" {
		t.Error("expected error result")
	}
}

func TestToolExecutor_saveImageNote_InvalidArgs(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "key1"}
	if res := exec.Execute(user, "u1", 1, "save_image_note", `invalid`); res == "" {
		t.Error("expected error result")
	}
	if res := exec.Execute(user, "u1", 1, "save_image_note", `{"image_url":""}`); res == "" {
		t.Error("expected empty url error")
	}
}

func TestToolExecutor_saveImageNote_NotBound(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	user := &model.User{ChannelUserID: "u1", Status: "unbound"}
	res := exec.Execute(user, "u1", 1, "save_image_note", `{"image_url":"http://img"}`)
	if res != "尚未绑定喵滴 Key，请先绑定" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestGetNowTitle_Null(t *testing.T) {
	if getNowTitle("null") == "" {
		t.Error("expected default date title")
	}
}

func TestMaskKey_Short(t *testing.T) {
	if maskKey("abc") != "***" {
		t.Errorf("unexpected mask: %s", maskKey("abc"))
	}
}

func TestToolExecutor_bindMiaodiKey_DBError(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("UPDATE agent_users SET apikey").WithArgs("key1", "bound", "u1").WillReturnError(sqlmock.ErrCancelled)
	user := &model.User{ChannelUserID: "u1", Status: "unbound"}
	res := exec.Execute(user, "u1", 1, "bind_miaodi_key", `{"key":"key1"}`)
	if res != "绑定失败：数据库错误" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_setSavePath_DBError(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("UPDATE agent_users SET book").WithArgs("b", "c", "t", "u1").WillReturnError(sqlmock.ErrCancelled)
	user := &model.User{ChannelUserID: "u1"}
	res := exec.Execute(user, "u1", 1, "set_save_path", `{"book":"b","chapter":"c","title":"t"}`)
	if res != "设置失败：数据库错误" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_saveImageNote_DBError(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("INSERT INTO pending_images").WithArgs("key1", "http://img", "b", "c", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnError(sqlmock.ErrCancelled)
	user := &model.User{ChannelUserID: "u1", Status: "bound", APIKey: "key1", Book: "b", Chara: "c"}
	res := exec.Execute(user, "u1", 1, "save_image_note", `{"image_url":"http://img"}`)
	if res == "" {
		t.Error("expected error result")
	}
}

func TestGetNowTitle_Empty(t *testing.T) {
	if getNowTitle("") == "" {
		t.Error("expected default date title")
	}
}

func TestToolExecutor_resetConversation(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("DELETE FROM agent_conversations").WithArgs("u1", int64(100)).WillReturnResult(sqlmock.NewResult(0, 1))

	user := &model.User{ChannelUserID: "u1"}
	res := exec.Execute(user, "u1", 100, "reset_conversation", "{}")
	if res != "已清空当前会话，我们可以重新开始。" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_showHelp(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 100, "show_help", "{}")
	if res == "" {
		t.Error("expected help content")
	}
}

func TestToolExecutor_listRecentNotes(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)).
		AddRow("save_image_pending", time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC))
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnRows(rows)

	res := exec.Execute(&model.User{}, "u1", 100, "list_recent_notes", "{}")
	if res == "" || res == "查询失败" {
		t.Errorf("unexpected result: %s", res)
	}
	if !strings.Contains(res, "文本笔记") {
		t.Errorf("expected translated action label, got: %s", res)
	}
}

func TestToolExecutor_listRecentNotes_FiltersNonNotes(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("bind_key", time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC))
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnRows(rows)

	res := exec.Execute(&model.User{}, "u1", 100, "list_recent_notes", "{}")
	if res != "最近没有保存记录。" {
		t.Errorf("expected no records after filtering, got: %s", res)
	}
}

func TestToolExecutor_queryNotesByDate(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("put_text", time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC))
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND created_at >= \? AND created_at < \?`).
		WithArgs("u1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(rows)

	res := exec.Execute(&model.User{}, "u1", 100, "query_notes_by_date", `{"date":"2026-06-30"}`)
	if res == "" || res == "查询失败" {
		t.Errorf("unexpected result: %s", res)
	}
	if !strings.Contains(res, "文本笔记") {
		t.Errorf("expected translated action label, got: %s", res)
	}
}

func TestToolExecutor_queryNotesByDate_MissingDate(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 100, "query_notes_by_date", "{}")
	if res != "date 不能为空" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestFormatAction(t *testing.T) {
	cases := map[string]string{
		"put_text":           "文本笔记",
		"save_image_pending": "图片笔记",
		"put_text_failed":    "文本保存失败",
		"save_image_failed":  "图片保存失败",
		"bind_key":           "绑定 Key",
		"something_unknown":  "something_unknown",
	}
	for in, want := range cases {
		if got := formatAction(in); got != want {
			t.Errorf("formatAction(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsNoteAction(t *testing.T) {
	for _, a := range []string{"put_text", "save_image_pending", "put_text_failed", "save_image_failed"} {
		if !isNoteAction(a) {
			t.Errorf("isNoteAction(%q) = false, want true", a)
		}
	}
	if isNoteAction("bind_key") {
		t.Error(`isNoteAction("bind_key") = true, want false`)
	}
}

func TestGetNowTitle_Passthrough(t *testing.T) {
	if got := getNowTitle("我的标题"); got != "我的标题" {
		t.Errorf("expected passthrough title, got: %s", got)
	}
}

func TestToolExecutor_resetConversation_NilRepo(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	exec.convRepo = nil
	res := exec.Execute(&model.User{ChannelUserID: "u1"}, "u1", 100, "reset_conversation", "{}")
	if res != "重置失败：会话仓库未初始化" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_resetConversation_ClearError(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectExec("DELETE FROM agent_conversations").WithArgs("u1", int64(100)).WillReturnError(sqlmock.ErrCancelled)
	res := exec.Execute(&model.User{ChannelUserID: "u1"}, "u1", 100, "reset_conversation", "{}")
	if !strings.HasPrefix(res, "重置失败：") {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_queryNotesByDate_InvalidArgs(t *testing.T) {
	exec, _ := newToolExecutorMock(t)
	res := exec.Execute(&model.User{}, "u1", 100, "query_notes_by_date", `invalid`)
	if res != "参数解析失败" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_queryNotesByDate_QueryError(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND created_at >= \? AND created_at < \?`).
		WithArgs("u1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnError(sqlmock.ErrCancelled)
	res := exec.Execute(&model.User{}, "u1", 100, "query_notes_by_date", `{"date":"2026-06-30"}`)
	if !strings.HasPrefix(res, "查询失败：") {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_queryNotesByDate_FiltersNonNotes(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	rows := sqlmock.NewRows([]string{"action", "created_at"}).
		AddRow("bind_key", time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC))
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log WHERE channel_user_id = \? AND created_at >= \? AND created_at < \?`).
		WithArgs("u1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(rows)
	res := exec.Execute(&model.User{}, "u1", 100, "query_notes_by_date", `{"date":"2026-06-30"}`)
	if res != "2026-06-30 没有保存记录。" {
		t.Errorf("unexpected result: %s", res)
	}
}

func TestToolExecutor_listRecentNotes_QueryError(t *testing.T) {
	exec, mock := newToolExecutorMock(t)
	mock.ExpectQuery(`SELECT action, created_at FROM api_call_log`).WithArgs("u1", 5).WillReturnError(sqlmock.ErrCancelled)
	res := exec.Execute(&model.User{}, "u1", 100, "list_recent_notes", "{}")
	if !strings.HasPrefix(res, "查询失败：") {
		t.Errorf("unexpected result: %s", res)
	}
}
