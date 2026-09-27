// Package auth issues and verifies the dashboard session JWT. Auth.js
// in web/ owns the OAuth flow; this package signs the post-auth token
// the dashboard then sends back. Phase 5 retires the X-Optiqor-Tenant
// header extractor in favour of the JWT extractor wired here.
package auth
