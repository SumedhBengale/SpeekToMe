package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticatedEventsValidateAccountInstanceAndAge(t *testing.T) {
	cloud := &fakeCloud{}
	o, _ := NewController(testConfig(t), cloud, &fakeProbe{})
	defer o.Stop()
	o.Reconcile(context.Background())
	event := AWSEventBridgeEvent{Source: "aws.ec2", Account: "123", Region: "us-west-2", DetailType: "EC2 Spot Instance Interruption Warning", Time: time.Now(), Detail: json.RawMessage(`{"instance-id":"i-1"}`)}
	b, _ := json.Marshal(event)
	req := httptest.NewRequest("POST", "/webhooks/aws-spot-event", bytes.NewReader(b))
	w := httptest.NewRecorder()
	o.Handler().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("unauthenticated callback accepted")
	}
	req = httptest.NewRequest("POST", "/webhooks/aws-spot-event", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer 012345678901234567890123456789012345")
	w = httptest.NewRecorder()
	o.Handler().ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	event.Account = "other"
	if o.ProcessEvent(event) == nil {
		t.Fatal("wrong account accepted")
	}
	event.Account = "123"
	event.Time = time.Now().Add(-time.Hour)
	if o.ProcessEvent(event) == nil {
		t.Fatal("stale event accepted")
	}
	event.Time = time.Now()
	event.Detail = json.RawMessage(`{"instance-id":"i-unmanaged"}`)
	if o.ProcessEvent(event) == nil {
		t.Fatal("unmanaged event altered fleet")
	}
}
func TestOldSNSSubscriptionPayloadCannotCauseSSRF(t *testing.T) {
	o, _ := NewController(testConfig(t), &fakeCloud{}, &fakeProbe{})
	defer o.Stop()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/aws-spot-event", bytes.NewBufferString(`{"Type":"SubscriptionConfirmation","SubscribeURL":"http://169.254.169.254/"}`))
	req.Header.Set("Authorization", "Bearer 012345678901234567890123456789012345")
	w := httptest.NewRecorder()
	o.Handler().ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatal("SNS payload accepted")
	}
}
