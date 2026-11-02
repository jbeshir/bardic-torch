package domain

// AuthMethod is how a request authenticated.
type AuthMethod string

const (
	// AuthMethodAuth0 is an Auth0 access token, sent as "Bearer auth0|<jwt>".
	AuthMethodAuth0 AuthMethod = "auth0"
	// AuthMethodAPIToken is a personal API token, sent as "Bearer user_api|<secret>".
	AuthMethodAPIToken AuthMethod = "api_token"
)

// Principal is the authenticated user of a request and how they authenticated.
type Principal struct {
	User   User
	Method AuthMethod
}
