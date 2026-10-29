package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	csrfCookieName    = "__Host-curu_csrf"
	csrfFormField     = "csrf_token"
	csrfHeaderName    = "X-CSRF-Token"
	maxRequestBody    = 1 << 20
	htmxAuthEventName = "curu:authentication-required"
)

// BrowserConfig supplies the explicit public and trusted-gateway boundary.
type BrowserConfig struct {
	PublicOrigin      string
	TrustedProxyCIDRs []netip.Prefix
	LoginURL          string
	LogoutURL         string
}

type csrfContextKey struct{}
type clientAddressContextKey struct{}

// CSRFTokenFromRequest returns the token established for a safe, authorized
// browser response. Server-rendered forms put this value in csrf_token.
func CSRFTokenFromRequest(r *http.Request) string {
	token, _ := r.Context().Value(csrfContextKey{}).(string)
	return token
}

// ClientAddressFromRequest returns the direct peer unless that peer is in an
// explicitly trusted proxy CIDR and supplied one valid, overwritten address.
func ClientAddressFromRequest(r *http.Request) (netip.Addr, bool) {
	address, ok := r.Context().Value(clientAddressContextKey{}).(netip.Addr)
	return address, ok
}

func (h *handler) protectBrowserRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}

		r = r.WithContext(contextWithClientAddress(r, h.browser.TrustedProxyCIDRs))
		if isSafeMethod(r.Method) {
			token := validCSRFCookie(r)
			if token == "" {
				var err error
				token, err = newCSRFToken()
				if err != nil {
					h.deny(w, r, http.StatusServiceUnavailable, "csrf_unavailable")
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name: csrfCookieName, Value: token, Path: "/", Secure: true,
					HttpOnly: true, SameSite: http.SameSiteLaxMode,
				})
			}
			next.ServeHTTP(w, r.WithContext(withCSRFToken(r, token)))
			return
		}

		if !h.validBrowserOrigin(r) {
			h.deny(w, r, http.StatusForbidden, "browser_origin_rejected")
			return
		}
		fetchSites := r.Header.Values("Sec-Fetch-Site")
		if len(fetchSites) > 1 || (len(fetchSites) == 1 && fetchSites[0] != "same-origin") {
			h.deny(w, r, http.StatusForbidden, "browser_origin_rejected")
			return
		}

		provided, status := csrfTokenFromMutation(r)
		if status != 0 {
			h.rejectInput(w, status)
			return
		}
		cookie := validCSRFCookie(r)
		if cookie == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(provided)) != 1 {
			h.deny(w, r, http.StatusForbidden, "csrf_rejected")
			return
		}
		next.ServeHTTP(w, r.WithContext(withCSRFToken(r, cookie)))
	})
}

func (h *handler) validBrowserOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	return len(values) == 1 && values[0] == h.browser.PublicOrigin
}

func (h *handler) rejectInput(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(http.StatusText(status) + "\n"))
}

func csrfTokenFromMutation(r *http.Request) (string, int) {
	values := r.Header.Values(csrfHeaderName)
	if len(values) > 1 {
		return "", http.StatusBadRequest
	}
	if len(values) == 1 {
		return values[0], 0
	}

	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || len(parameters) != 0 {
		return "", http.StatusUnsupportedMediaType
	}
	if err := r.ParseForm(); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return "", http.StatusRequestEntityTooLarge
		}
		return "", http.StatusBadRequest
	}
	values = r.PostForm[csrfFormField]
	if len(values) != 1 {
		return "", http.StatusBadRequest
	}
	return values[0], 0
}

func validCSRFCookie(r *http.Request) string {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == csrfCookieName {
			value = cookie.Value
			count++
		}
	}
	if count != 1 || !validCSRFToken(value) {
		return ""
	}
	return value
}

func validCSRFToken(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}

func newCSRFToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func withCSRFToken(r *http.Request, token string) context.Context {
	return context.WithValue(r.Context(), csrfContextKey{}, token)
}

func contextWithClientAddress(r *http.Request, trusted []netip.Prefix) context.Context {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return r.Context()
	}
	client := peer.Unmap()
	for _, prefix := range trusted {
		if !prefix.Contains(peer) {
			continue
		}
		values := r.Header.Values("X-Forwarded-For")
		if len(values) == 1 && !strings.Contains(values[0], ",") {
			if forwarded, err := netip.ParseAddr(values[0]); err == nil {
				client = forwarded.Unmap()
			}
		}
		break
	}
	return context.WithValue(r.Context(), clientAddressContextKey{}, client)
}
