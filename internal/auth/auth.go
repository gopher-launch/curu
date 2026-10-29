// Package auth validates gateway-supplied OAuth JWT access tokens without
// exposing tokens or provider claims to application and domain code.
package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
)

const (
	maxTokenSize    = 64 << 10
	maxJWKSBodySize = 1 << 20
	maxCacheTTL     = 15 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrKeyService         = errors.New("signing-key service unavailable")
)

// Principal is the stable external identity proven by a validated access
// token. Authorization maps it to application authority in CURU-020.
type Principal struct {
	Issuer  string
	Subject string
}

type Config struct {
	Profile                   string
	Issuer                    string
	Audience                  string
	JWKSURL                   string
	Algorithms                []string
	ClockSkew                 time.Duration
	MaxLifetime               time.Duration
	FetchTimeout              time.Duration
	DefaultCacheTTL           time.Duration
	UnknownKIDRefreshInterval time.Duration
}

type Authenticator struct {
	config     Config
	client     *http.Client
	now        func() time.Time
	algorithms map[string]jwa.SignatureAlgorithm

	mu                 sync.Mutex
	keys               jwk.Set
	keysExpireAt       time.Time
	lastUnknownRefresh time.Time
	readyUntil         atomic.Int64
}

// New constructs the explicit RFC 9068 authentication boundary. Configuration
// is expected to have passed internal/config validation.
func New(config Config, client *http.Client) (*Authenticator, error) {
	if config.Profile != "rfc9068" {
		return nil, fmt.Errorf("unsupported access-token profile")
	}
	if client == nil {
		client = &http.Client{CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" {
				return fmt.Errorf("JWKS redirect must remain HTTPS")
			}
			if len(via) > 0 && !strings.EqualFold(request.URL.Host, via[0].URL.Host) {
				return fmt.Errorf("JWKS redirect must remain on the configured host")
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many JWKS redirects")
			}
			return nil
		}}
	}
	algorithms := make(map[string]jwa.SignatureAlgorithm, len(config.Algorithms))
	for _, name := range config.Algorithms {
		algorithm, ok := jwa.LookupSignatureAlgorithm(name)
		if !ok || (name != "RS256" && name != "ES256") {
			return nil, fmt.Errorf("unsupported JWT signing algorithm")
		}
		algorithms[name] = algorithm
	}
	return &Authenticator{config: config, client: client, now: time.Now, algorithms: algorithms}, nil
}

// AuthenticateRequest accepts exactly one Bearer credential from the
// Authorization header. It never reads tokens from cookies or query values.
func (a *Authenticator) AuthenticateRequest(ctx context.Context, headerValues []string) (Principal, error) {
	if len(headerValues) != 1 {
		return Principal{}, ErrInvalidCredentials
	}
	fields := strings.Fields(headerValues[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || fields[1] == "" {
		return Principal{}, ErrInvalidCredentials
	}
	return a.Validate(ctx, fields[1])
}

// Validate verifies the compact token before returning its stable identity.
func (a *Authenticator) Validate(ctx context.Context, compact string) (Principal, error) {
	if len(compact) == 0 || len(compact) > maxTokenSize {
		return Principal{}, ErrInvalidCredentials
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Principal{}, ErrInvalidCredentials
	}
	headerBytes, ok := decodeBase64URLSegment(parts[0])
	if !ok {
		return Principal{}, ErrInvalidCredentials
	}
	if _, ok := decodeBase64URLSegment(parts[1]); !ok {
		return Principal{}, ErrInvalidCredentials
	}
	if _, ok := decodeBase64URLSegment(parts[2]); !ok {
		return Principal{}, ErrInvalidCredentials
	}
	header, err := decodeUniqueObject(headerBytes)
	if err != nil {
		return Principal{}, ErrInvalidCredentials
	}
	if !validJWTEncodingHeaders(header) {
		return Principal{}, ErrInvalidCredentials
	}
	algorithmName, ok := requiredString(header, "alg")
	if !ok {
		return Principal{}, ErrInvalidCredentials
	}
	algorithm, allowed := a.algorithms[algorithmName]
	if !allowed {
		return Principal{}, ErrInvalidCredentials
	}
	kid, ok := requiredString(header, "kid")
	if !ok {
		return Principal{}, ErrInvalidCredentials
	}
	typ, ok := requiredString(header, "typ")
	if !ok || (!strings.EqualFold(typ, "at+jwt") && !strings.EqualFold(typ, "application/at+jwt")) {
		return Principal{}, ErrInvalidCredentials
	}

	key, err := a.key(ctx, kid, algorithmName)
	if err != nil {
		return Principal{}, err
	}
	payload, err := jws.Verify([]byte(compact), jws.WithKey(algorithm, key))
	if err != nil {
		return Principal{}, ErrInvalidCredentials
	}
	claims, err := decodeUniqueObject(payload)
	if err != nil {
		return Principal{}, ErrInvalidCredentials
	}
	return a.validateClaims(claims)
}

func decodeBase64URLSegment(segment string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(segment)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != segment {
		return nil, false
	}
	return decoded, true
}

func validJWTEncodingHeaders(header map[string]json.RawMessage) bool {
	if raw, present := header["b64"]; present {
		var encoded bool
		if json.Unmarshal(raw, &encoded) != nil || !encoded {
			return false
		}
	}
	raw, present := header["crit"]
	if !present {
		return true
	}
	var critical []string
	if json.Unmarshal(raw, &critical) != nil || len(critical) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(critical))
	for _, name := range critical {
		if name != "b64" {
			return false
		}
		if _, duplicate := seen[name]; duplicate {
			return false
		}
		seen[name] = struct{}{}
	}
	_, hasB64 := header["b64"]
	return hasB64
}

// Refresh retrieves trusted keys immediately. A failed refresh leaves a
// still-fresh prior set available but never extends its expiry.
func (a *Authenticator) Refresh(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fetchLocked(ctx)
}

// Maintain keeps the configured key set warm until ctx is canceled. Failures
// are reported without exposing endpoint or response details to routine logs.
func (a *Authenticator) Maintain(ctx context.Context, failed func()) {
	for {
		err := a.Refresh(ctx)
		if ctx.Err() != nil {
			return
		}
		delay := a.config.UnknownKIDRefreshInterval
		if err != nil {
			if failed != nil {
				failed()
			}
			a.mu.Lock()
			remaining := a.keysExpireAt.Sub(a.now())
			a.mu.Unlock()
			if retry := remaining / 2; retry > 0 && retry < delay {
				delay = retry
			}
		} else {
			a.mu.Lock()
			delay = a.keysExpireAt.Sub(a.now()) / 2
			a.mu.Unlock()
		}
		if delay < time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Ready reports whether a successfully retrieved, unexpired key set is
// available. It does not perform network I/O.
func (a *Authenticator) Ready() bool {
	return a.now().UnixNano() < a.readyUntil.Load()
}

func (a *Authenticator) key(ctx context.Context, kid, algorithm string) (jwk.Key, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.keys == nil || !now.Before(a.keysExpireAt) {
		if err := a.fetchLocked(ctx); err != nil {
			return nil, ErrKeyService
		}
	}
	key, count := eligibleKey(a.keys, kid, algorithm)
	if count == 1 {
		return key, nil
	}
	if count > 1 {
		return nil, ErrInvalidCredentials
	}
	if !a.lastUnknownRefresh.IsZero() && now.Sub(a.lastUnknownRefresh) < a.config.UnknownKIDRefreshInterval {
		return nil, ErrInvalidCredentials
	}
	a.lastUnknownRefresh = now
	if err := a.fetchLocked(ctx); err != nil {
		if a.keys == nil || !a.now().Before(a.keysExpireAt) {
			return nil, ErrKeyService
		}
		return nil, ErrInvalidCredentials
	}
	key, count = eligibleKey(a.keys, kid, algorithm)
	if count != 1 {
		return nil, ErrInvalidCredentials
	}
	return key, nil
}

func eligibleKey(set jwk.Set, kid, algorithm string) (jwk.Key, int) {
	var selected jwk.Key
	count := 0
	for index := 0; index < set.Len(); index++ {
		key, ok := set.Key(index)
		if !ok {
			continue
		}
		keyID, present := key.KeyID()
		if !present || keyID != kid || !keyEligible(key, algorithm) {
			continue
		}
		selected = key
		count++
	}
	return selected, count
}

func keyEligible(key jwk.Key, algorithm string) bool {
	if err := key.Validate(); err != nil {
		return false
	}
	if use, ok := key.KeyUsage(); ok && use != "sig" {
		return false
	}
	if operations, ok := key.KeyOps(); ok {
		canVerify := false
		for _, operation := range operations {
			canVerify = canVerify || operation == jwk.KeyOpVerify
		}
		if !canVerify {
			return false
		}
	}
	if declared, ok := key.Algorithm(); ok && declared.String() != algorithm {
		return false
	}
	if algorithm == "RS256" && key.KeyType() != jwa.RSA() {
		return false
	}
	if algorithm == "ES256" && key.KeyType() != jwa.EC() {
		return false
	}
	return true
}

func (a *Authenticator) fetchLocked(ctx context.Context) error {
	fetchCtx, cancel := context.WithTimeout(ctx, a.config.FetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, a.config.JWKSURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	requestTime := a.now()
	response, err := a.client.Do(request)
	if err != nil {
		return err
	}
	responseTime := a.now()
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("JWKS response status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSBodySize+1))
	if err != nil || len(body) > maxJWKSBodySize {
		return fmt.Errorf("invalid JWKS response size")
	}
	set, err := jwk.Parse(body)
	if err != nil || !a.hasUsableKey(set) {
		return fmt.Errorf("invalid JWKS response")
	}
	cacheTime := a.now()
	ttl := cacheTTL(response.Header, requestTime, responseTime, cacheTime, a.config.DefaultCacheTTL)
	if ttl <= 0 {
		return fmt.Errorf("JWKS response is not fresh")
	}
	a.keys = set
	a.keysExpireAt = cacheTime.Add(ttl)
	a.readyUntil.Store(a.keysExpireAt.UnixNano())
	return nil
}

func (a *Authenticator) hasUsableKey(set jwk.Set) bool {
	counts := make(map[string]int)
	for index := 0; index < set.Len(); index++ {
		key, ok := set.Key(index)
		if !ok {
			continue
		}
		kid, present := key.KeyID()
		if !present || kid == "" {
			continue
		}
		for algorithm := range a.algorithms {
			if keyEligible(key, algorithm) {
				counts[kid]++
				break
			}
		}
	}
	for _, count := range counts {
		if count == 1 {
			return true
		}
	}
	return false
}

func cacheTTL(header http.Header, requestTime, responseTime, now time.Time, fallback time.Duration) time.Duration {
	freshness := fallback
	maxAgeValues := make([]string, 0, 1)
	directives, valid := cacheControlDirectives(header.Values("Cache-Control"))
	if !valid {
		return 0
	}
	for _, directive := range directives {
		name, value, found := strings.Cut(strings.TrimSpace(directive), "=")
		name = strings.TrimSpace(name)
		switch {
		case strings.EqualFold(name, "no-store"), strings.EqualFold(name, "no-cache"):
			return 0
		case strings.EqualFold(name, "max-age"):
			if !found {
				return 0
			}
			maxAgeValues = append(maxAgeValues, strings.TrimSpace(value))
		}
	}
	if len(maxAgeValues) > 1 {
		return 0
	}
	if len(maxAgeValues) == 1 {
		seconds, valid := parseDeltaSeconds(maxAgeValues[0])
		if !valid {
			return 0
		}
		freshness = secondsDuration(seconds)
	} else if expiresValues := header.Values("Expires"); len(expiresValues) > 0 {
		if len(expiresValues) != 1 {
			return 0
		}
		expires, err := http.ParseTime(expiresValues[0])
		if err != nil {
			return 0
		}
		date := responseTime
		if dateValues := header.Values("Date"); len(dateValues) > 0 {
			if len(dateValues) != 1 {
				return 0
			}
			date, err = http.ParseTime(dateValues[0])
			if err != nil {
				return 0
			}
		}
		freshness = expires.Sub(date)
	}

	currentAge, valid := responseAge(header, requestTime, responseTime, now)
	if !valid || freshness <= currentAge {
		return 0
	}
	ttl := freshness - currentAge
	if ttl > maxCacheTTL {
		return maxCacheTTL
	}
	return ttl
}

func cacheControlDirectives(values []string) ([]string, bool) {
	var directives []string
	for _, value := range values {
		start := 0
		quoted := false
		escaped := false
		for index, char := range value {
			if escaped {
				escaped = false
				continue
			}
			if quoted && char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				quoted = !quoted
				continue
			}
			if char == ',' && !quoted {
				directives = append(directives, value[start:index])
				start = index + 1
			}
		}
		if quoted || escaped {
			return nil, false
		}
		directives = append(directives, value[start:])
	}
	return directives, true
}

func parseDeltaSeconds(value string) (uint64, bool) {
	if strings.HasPrefix(value, `"`) || strings.HasSuffix(value, `"`) {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return 0, false
		}
		value = unquoted
	} else if strings.Contains(value, `"`) {
		return 0, false
	}
	seconds, err := strconv.ParseUint(value, 10, 63)
	return seconds, err == nil
}

func responseAge(header http.Header, requestTime, responseTime, now time.Time) (time.Duration, bool) {
	apparentAge := time.Duration(0)
	if dateValues := header.Values("Date"); len(dateValues) > 0 {
		if len(dateValues) != 1 {
			return 0, false
		}
		date, err := http.ParseTime(dateValues[0])
		if err != nil {
			return 0, false
		}
		if responseTime.After(date) {
			apparentAge = responseTime.Sub(date)
		}
	}
	responseDelay := responseTime.Sub(requestTime)
	if responseDelay < 0 {
		responseDelay = 0
	}
	ageValue := time.Duration(0)
	if values := header.Values("Age"); len(values) > 0 {
		first := strings.TrimSpace(strings.Split(values[0], ",")[0])
		ageValue = ageDuration(first)
	}
	correctedAge := ageValue + responseDelay
	if correctedAge < ageValue {
		correctedAge = time.Duration(math.MaxInt64)
	}
	if apparentAge > correctedAge {
		correctedAge = apparentAge
	}
	residentTime := now.Sub(responseTime)
	if residentTime < 0 {
		residentTime = 0
	}
	currentAge := correctedAge + residentTime
	if currentAge < correctedAge {
		return time.Duration(math.MaxInt64), true
	}
	return currentAge, true
}

func ageDuration(value string) time.Duration {
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err == nil {
		return secondsDuration(seconds)
	}
	if value == "" {
		return 0
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0
		}
	}
	// RFC 9111 requires syntactically numeric delta-seconds that overflow
	// local representation to saturate rather than wrap or become zero.
	return secondsDuration(1 << 31)
}

func secondsDuration(seconds uint64) time.Duration {
	if seconds > uint64(math.MaxInt64/int64(time.Second)) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds) * time.Second
}

func (a *Authenticator) validateClaims(claims map[string]json.RawMessage) (Principal, error) {
	issuer, ok := requiredString(claims, "iss")
	if !ok || issuer != a.config.Issuer {
		return Principal{}, ErrInvalidCredentials
	}
	subject, ok := requiredString(claims, "sub")
	if !ok {
		return Principal{}, ErrInvalidCredentials
	}
	if !audienceContains(claims["aud"], a.config.Audience) {
		return Principal{}, ErrInvalidCredentials
	}
	if _, ok := requiredString(claims, "client_id"); !ok {
		return Principal{}, ErrInvalidCredentials
	}
	if _, ok := requiredString(claims, "jti"); !ok {
		return Principal{}, ErrInvalidCredentials
	}
	issuedAt, ok := numericDate(claims["iat"])
	if !ok {
		return Principal{}, ErrInvalidCredentials
	}
	expiresAt, ok := numericDate(claims["exp"])
	if !ok || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > a.config.MaxLifetime {
		return Principal{}, ErrInvalidCredentials
	}
	now := a.now()
	if now.After(expiresAt.Add(a.config.ClockSkew)) || issuedAt.After(now.Add(a.config.ClockSkew)) {
		return Principal{}, ErrInvalidCredentials
	}
	if raw, present := claims["nbf"]; present {
		notBefore, valid := numericDate(raw)
		if !valid || notBefore.After(now.Add(a.config.ClockSkew)) {
			return Principal{}, ErrInvalidCredentials
		}
	}
	return Principal{Issuer: issuer, Subject: subject}, nil
}

func requiredString(object map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := object[name]
	if !ok {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" {
		return "", false
	}
	return value, true
}

func audienceContains(raw json.RawMessage, expected string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == expected
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) != nil || len(multiple) == 0 {
		return false
	}
	for _, audience := range multiple {
		if audience == expected {
			return true
		}
	}
	return false
}

func numericDate(raw json.RawMessage) (time.Time, bool) {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) != nil {
		return time.Time{}, false
	}
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return time.Time{}, false
	}
	seconds, fraction := math.Modf(value)
	return time.Unix(int64(seconds), int64(fraction*float64(time.Second))), true
}

func decodeUniqueObject(data []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid UTF-8 JSON")
	}
	if !validJSONSurrogateEscapes(data) {
		return nil, fmt.Errorf("invalid JSON Unicode escape")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, fmt.Errorf("expected JSON object")
	}
	result := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("invalid JSON member name")
		}
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("duplicate JSON member")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		result[name] = value
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return nil, fmt.Errorf("invalid JSON object")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return result, nil
}

func validJSONSurrogateEscapes(data []byte) bool {
	inString := false
	for index := 0; index < len(data); index++ {
		switch data[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			index++
			if index >= len(data) || data[index] != 'u' {
				continue
			}
			value, ok := jsonHexEscape(data, index)
			if !ok {
				return false
			}
			index += 4
			if value >= 0xdc00 && value <= 0xdfff {
				return false
			}
			if value < 0xd800 || value > 0xdbff {
				continue
			}
			if index+6 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' {
				return false
			}
			low, ok := jsonHexEscape(data, index+2)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			index += 6
		}
	}
	return true
}

func jsonHexEscape(data []byte, uIndex int) (uint16, bool) {
	if uIndex+4 >= len(data) {
		return 0, false
	}
	var value uint16
	for _, digit := range data[uIndex+1 : uIndex+5] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value += uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value += uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value += uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
