//go:build integration

package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
	"github.com/stretchr/testify/require"
)

// The contact form and its inbox.
const (
	contactPath         = "/v1/contact"
	contactRequestsPath = "/v1/moderation/contact-requests"
)

// passingTurnstile is the contact form's Turnstile verifier in the HTTP integration tests: every token passes,
// and Cloudflare is never called.
type passingTurnstile struct{}

func (passingTurnstile) VerifyTurnstile(context.Context, string) error { return nil }

// contactBody is the body of a contact request of kind with the given name, email address and message, and the
// dummy token Cloudflare's test site keys produce.
func contactBody(kind, name, email, message string) string {
	return `{"kind":"` + kind + `","name":"` + name + `","email":"` + email + `","message":"` + message +
		`","url":null,"turnstile_token":"XXXX.DUMMY.TOKEN.XXXX"}`
}

// sendContact sends a contact request anonymously and requires it to be received.
func sendContact(t *testing.T, e endToEnd, body string) {
	t.Helper()
	received := sendOK[controller.ContactReceived](t, e, http.MethodPost, contactPath, "", body, http.StatusAccepted)
	require.Equal(t, "received", received.Status)
}

// contactRequestWith returns the request of the inbox list whose message is message.
func contactRequestWith(t *testing.T, list jsonObject, message string) jsonObject {
	t.Helper()
	data, ok := list["data"].([]any)
	require.True(t, ok, "data is a list")
	for _, item := range data {
		request, ok := item.(jsonObject)
		if ok && request["message"] == message {
			return request
		}
	}
	require.Fail(t, "contact request not listed")
	return nil
}
