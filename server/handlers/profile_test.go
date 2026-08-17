package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"speektome-server/db"
)

func TestProfileSummaryAndLifecycle(t *testing.T) {
	databasePath := t.TempDir() + string(os.PathSeparator) + "profile_test.db"
	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer database.Close()

	// 1. Create a user
	userID, err := database.CreateEmailUser("testprofile@example.com", "Password123", "Test Profile User", "token123")
	if err != nil {
		t.Fatalf("CreateEmailUser: %v", err)
	}

	// 2. Add some usage logs
	err = database.LogUsageAndUpdateQuota(userID, 45.0, 1.2, 0.5, 1.7, true, "llama-3-8b", "hello world testing", "Hello, world! Testing.", "success", 1024, "groq", "groq")
	if err != nil {
		t.Fatalf("LogUsageAndUpdateQuota: %v", err)
	}

	// 3. Add a meeting
	meetingID, err := database.CreateMeeting(userID, "Sprint Planning", 120.0, "")
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	err = database.UpdateMeetingResult(meetingID, "Speaker 1: Let us plan sprint items.", "Sprint planning meeting summary", "- Item 1\n- Item 2", "[]")
	if err != nil {
		t.Fatalf("UpdateMeetingResult: %v", err)
	}

	// 4. Add a vocabulary term
	_, err = database.AddVocabularyTerm(userID, "SpeekToMe", "SpeekTo.Me")
	if err != nil {
		t.Fatalf("AddVocabularyTerm: %v", err)
	}

	// 5. Test GetUserProfileSummary
	summary, err := database.GetUserProfileSummary(userID)
	if err != nil {
		t.Fatalf("GetUserProfileSummary: %v", err)
	}

	if summary.Email != "testprofile@example.com" {
		t.Errorf("Expected email testprofile@example.com, got %s", summary.Email)
	}
	if summary.TotalTranscriptions != 1 {
		t.Errorf("Expected 1 transcription, got %d", summary.TotalTranscriptions)
	}
	if summary.TotalAudioDuration != 45.0 {
		t.Errorf("Expected 45.0 duration, got %f", summary.TotalAudioDuration)
	}
	if summary.TotalMeetings != 1 {
		t.Errorf("Expected 1 meeting, got %d", summary.TotalMeetings)
	}
	if summary.TotalMeetingDuration != 120.0 {
		t.Errorf("Expected 120.0 meeting duration, got %f", summary.TotalMeetingDuration)
	}
	if summary.TotalVocabularyTerms != 1 {
		t.Errorf("Expected 1 vocabulary term, got %d", summary.TotalVocabularyTerms)
	}
	if summary.TotalEnhancements != 1 {
		t.Errorf("Expected 1 enhancement, got %d", summary.TotalEnhancements)
	}
	if summary.TotalWordsTranscribed <= 0 {
		t.Errorf("Expected >0 words transcribed, got %d", summary.TotalWordsTranscribed)
	}

	// 6. Test Password Reset Token Flow
	resetToken := "secure_reset_token_test_12345"
	name, err := database.SetPasswordResetToken("testprofile@example.com", resetToken, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("SetPasswordResetToken: %v", err)
	}
	if name != "Test Profile User" {
		t.Errorf("Expected name 'Test Profile User', got '%s'", name)
	}

	// Validate token
	foundID, email, err := database.ValidatePasswordResetToken(resetToken)
	if err != nil {
		t.Fatalf("ValidatePasswordResetToken: %v", err)
	}
	if foundID != userID || email != "testprofile@example.com" {
		t.Errorf("ValidatePasswordResetToken returned id %d, email %s", foundID, email)
	}

	// Reset password with new password
	app := &App{DB: database}
	resetReqBody := `{"token":"` + resetToken + `","password":"NewPassword1234"}`
	resetReq := httptest.NewRequest(http.MethodPost, "/auth/reset-password", strings.NewReader(resetReqBody))
	resetRec := httptest.NewRecorder()
	app.HandleResetPassword(resetRec, resetReq)

	if resetRec.Code != http.StatusOK {
		t.Fatalf("HandleResetPassword failed with code %d: %s", resetRec.Code, resetRec.Body.String())
	}

	// Authenticate with new password
	authID, err := database.AuthenticateEmailUser("testprofile@example.com", "NewPassword1234")
	if err != nil {
		t.Fatalf("Authenticate with new password failed: %v", err)
	}
	if authID != userID {
		t.Errorf("Expected user ID %d, got %d", userID, authID)
	}

	// 7. Test Account Deletion
	sessionToken := "test_session_delete_token"
	err = database.CreateSession(sessionToken, userID, 0)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/auth/account", nil)
	delReq.Header.Set("Authorization", "Bearer "+sessionToken)
	delRec := httptest.NewRecorder()
	app.HandleDeleteAccount(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("HandleDeleteAccount failed with code %d: %s", delRec.Code, delRec.Body.String())
	}

	var delResp map[string]interface{}
	_ = json.NewDecoder(delRec.Body).Decode(&delResp)
	if delResp["status"] != "deleted" {
		t.Errorf("Expected status deleted, got %v", delResp["status"])
	}

	// Verify session and user no longer exist
	_, err = database.GetUserBySession(sessionToken)
	if err == nil {
		t.Errorf("Expected session to be deleted, but still found")
	}

	meetings, err := database.GetUserMeetings(userID)
	if err != nil || len(meetings) > 0 {
		t.Errorf("Expected 0 meetings after deletion, got %d (err: %v)", len(meetings), err)
	}
}
