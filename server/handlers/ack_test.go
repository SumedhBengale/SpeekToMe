package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"speektome-server/auth"
	"speektome-server/db"
)

func setupTestAppForAck(t *testing.T) (*App, *db.Database, string, int64, int64, string) {
	databasePath := t.TempDir() + string(os.PathSeparator) + "test_ack.db"
	t.Setenv("SESSION_SECRET", "test-ack-secret-for-security")

	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("NewDatabase failed: %v", err)
	}

	app := &App{DB: database}

	// Create user
	userID, err := database.CreateEmailUser("ackuser@example.com", "Password123", "Ack User", "verif_token")
	if err != nil {
		t.Fatalf("CreateEmailUser: %v", err)
	}

	sessionToken := auth.GenerateSessionToken()
	err = database.CreateSession(sessionToken, userID, 30)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Create usage log
	logID, err := database.LogUsageAndUpdateQuota(
		userID,
		5.0,
		0.5,
		0.2,
		0.7,
		false,
		"whisper-large-v3-turbo",
		"hello world testing",
		"Hello world testing",
		"success",
		1024,
		"groq",
		"",
	)
	if err != nil {
		t.Fatalf("LogUsageAndUpdateQuota: %v", err)
	}

	ackToken := auth.GenerateLogAckToken(userID, logID)
	return app, database, sessionToken, userID, logID, ackToken
}

func TestHandleTranscribeAck_Success(t *testing.T) {
	app, database, sessionToken, userID, logID, ackToken := setupTestAppForAck(t)
	defer database.Close()

	ackReq := TranscribeAckRequest{
		LogID:       logID,
		AckToken:    ackToken,
		FinalOutput: "Hello world testing with SpeekTo.Me vocabulary.",
	}
	body, _ := json.Marshal(ackReq)

	req := httptest.NewRequest("POST", "/v1/transcribe/ack", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	w := httptest.NewRecorder()

	app.HandleTranscribeAck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify database was updated
	logRec, err := database.GetUsageLogByID(logID, userID)
	if err != nil {
		t.Fatalf("GetUsageLogByID: %v", err)
	}
	if logRec.FinalOutput != "Hello world testing with SpeekTo.Me vocabulary." {
		t.Errorf("Expected updated final_output, got: %s", logRec.FinalOutput)
	}
}

func TestHandleTranscribeAck_TamperedTokenRejected(t *testing.T) {
	app, database, sessionToken, _, logID, _ := setupTestAppForAck(t)
	defer database.Close()

	ackReq := TranscribeAckRequest{
		LogID:       logID,
		AckToken:    "tampered_invalid_signature_hash",
		FinalOutput: "Injected text payload",
	}
	body, _ := json.Marshal(ackReq)

	req := httptest.NewRequest("POST", "/v1/transcribe/ack", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	w := httptest.NewRecorder()

	app.HandleTranscribeAck(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected status 403 Forbidden for tampered token, got %d", w.Code)
	}
}

func TestHandleTranscribeAck_OversizedPayloadRejected(t *testing.T) {
	app, database, sessionToken, _, logID, ackToken := setupTestAppForAck(t)
	defer database.Close()

	hugeText := string(make([]byte, (1<<20)+1))
	ackReq := TranscribeAckRequest{
		LogID:       logID,
		AckToken:    ackToken,
		FinalOutput: hugeText,
	}
	body, _ := json.Marshal(ackReq)

	req := httptest.NewRequest("POST", "/v1/transcribe/ack", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	w := httptest.NewRecorder()

	app.HandleTranscribeAck(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400 Bad Request for oversized payload, got %d", w.Code)
	}
}
