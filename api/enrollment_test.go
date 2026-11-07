package api_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/buildkite/agent/v4/api"
	agentedgev1 "github.com/buildkite/agent/v4/api/proto/gen"
	"github.com/buildkite/agent/v4/api/proto/gen/agentedgev1connect"
	"github.com/buildkite/agent/v4/internal/identity"
	"github.com/buildkite/agent/v4/logger"
)

// Verify with the standard library, independently of the JOSE signing code.
func verifyHostProof(req *http.Request, pub *ecdsa.PublicKey) (map[string]any, error) {
	parts := strings.Split(req.Header.Get("DPoP"), ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("missing proof")
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var h struct {
		Alg string            `json:"alg"`
		Typ string            `json:"typ"`
		JWK map[string]string `json:"jwk"`
	}
	if err := json.Unmarshal(header, &h); err != nil {
		return nil, err
	}
	point, err := pub.Bytes()
	if err != nil {
		return nil, err
	}
	x := base64.RawURLEncoding.EncodeToString(point[1:33])
	y := base64.RawURLEncoding.EncodeToString(point[33:])
	if h.Alg != "ES256" || h.Typ != "dpop+jwt" || len(h.JWK) != 4 ||
		h.JWK["kty"] != "EC" || h.JWK["crv"] != "P-256" || h.JWK["x"] != x || h.JWK["y"] != y {
		return nil, fmt.Errorf("unexpected proof key or header")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return nil, fmt.Errorf("invalid signature encoding")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return nil, fmt.Errorf("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	iat, ok := claims["iat"].(float64)
	if !ok || time.Since(time.Unix(int64(iat), 0)).Abs() > time.Minute ||
		claims["htm"] != req.Method || claims["htu"] != "https://"+req.Host+req.URL.EscapedPath() || claims["jti"] == "" {
		return nil, fmt.Errorf("invalid proof claims")
	}
	return claims, nil
}

func TestHostEnrollmentAndWorkerSessions(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Hardware backends expose crypto.Signer, not an ECDSA private key.
	key, err := identity.New(struct{ crypto.Signer }{private})
	if err != nil {
		t.Fatal(err)
	}
	// RFC 7638 canonical member order; do not obtain the expectation from Key.
	point, err := private.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	x := base64.RawURLEncoding.EncodeToString(point[1:33])
	y := base64.RawURLEncoding.EncodeToString(point[33:])
	hash := sha256.Sum256(fmt.Appendf(nil, `{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, x, y))
	jkt := base64.RawURLEncoding.EncodeToString(hash[:])
	if key.Thumbprint() != jkt {
		t.Fatal("incorrect host key thumbprint")
	}
	var mu sync.Mutex
	seen := make(map[string]bool)
	enrolled, registrations := false, 0
	streamHandler := connect.NewServerStreamHandler(agentedgev1connect.AgentEdgeServiceStreamPingsProcedure,
		func(_ context.Context, _ *connect.Request[agentedgev1.StreamPingsRequest], stream *connect.ServerStream[agentedgev1.StreamPingsResponse]) error {
			return stream.Send(&agentedgev1.StreamPingsResponse{Action: &agentedgev1.StreamPingsResponse_Disconnect{}})
		})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		claims, err := verifyHostProof(r, &private.PublicKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		jti, ok := claims["jti"].(string)
		if !ok || jti == "" || seen[jti] {
			http.Error(w, "replayed proof", http.StatusUnauthorized)
			return
		}
		seen[jti] = true
		if r.URL.Path == "/v3/enroll" {
			if r.Header.Get("Authorization") != "Token one-use-grant" || claims["ath"] != nil || enrolled {
				http.Error(w, "invalid enrollment", http.StatusUnauthorized)
				return
			}
			if claims["nonce"] != "enrollment-nonce" {
				w.Header().Set("DPoP-Nonce", "enrollment-nonce")
				http.Error(w, "use_dpop_nonce", http.StatusBadRequest)
				return
			}
			enrolled = true
			_ = json.NewEncoder(w).Encode(api.AgentEnrollmentResponse{AccessToken: "registration", TokenType: "DPoP", DPoPJKT: jkt})
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "DPoP ")
		tokenHash := sha256.Sum256([]byte(token))
		if !ok || claims["ath"] != base64.RawURLEncoding.EncodeToString(tokenHash[:]) {
			http.Error(w, "unbound token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v3/register":
			if token != "registration" || !enrolled {
				http.Error(w, "invalid registration", http.StatusUnauthorized)
				return
			}
			registrations++
			_ = json.NewEncoder(w).Encode(api.AgentRegisterResponse{
				UUID: fmt.Sprint(registrations), AccessToken: fmt.Sprintf("worker-%d", registrations),
				TokenType: "dpop", DPoPJKT: jkt, Endpoint: "https://" + r.Host + "/worker/",
			})
		case "/worker/connect", "/moved/connect":
			if token != "worker-1" && token != "worker-2" {
				http.Error(w, "invalid worker", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case agentedgev1connect.AgentEdgeServiceStreamPingsProcedure:
			if token != "worker-1" && token != "worker-2" {
				http.Error(w, "invalid worker", http.StatusUnauthorized)
				return
			}
			streamHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	conf := api.Config{
		Endpoint: server.URL + "/v3/", Token: "one-use-grant", HostKey: key,
		TLSConfig: server.Client().Transport.(*http.Transport).TLSClientConfig,
	}
	enrollmentClient := api.NewClient(logger.Discard, conf)
	enrollment, _, err := enrollmentClient.Enroll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := enrollmentClient.Enroll(t.Context()); err == nil {
		t.Fatal("enrollment grant was reusable")
	}
	conf.Token, conf.TokenType = enrollment.AccessToken, enrollment.TokenType
	registrar := api.NewClient(logger.Discard, conf)
	for range 2 {
		reg, _, err := registrar.Register(t.Context(), &api.AgentRegisterRequest{})
		if err != nil {
			t.Fatal(err)
		}
		worker := registrar.FromAgentRegisterResponse(reg)
		if _, err := worker.Connect(t.Context()); err != nil {
			t.Fatal(err)
		}
		moved := worker.FromPing(&api.Ping{Endpoint: server.URL + "/moved/"})
		if _, err := moved.Connect(t.Context()); err != nil {
			t.Fatal(err)
		}
		pings, err := moved.StreamPings(t.Context(), reg.UUID)
		if err != nil {
			t.Fatal(err)
		}
		messages := 0
		for msg, err := range pings {
			if err != nil || msg.GetDisconnect() == nil {
				t.Fatalf("bound ping stream = %v, %v", msg, err)
			}
			messages++
		}
		if messages != 1 {
			t.Fatalf("bound ping stream delivered %d messages, want 1", messages)
		}
		// Copy the token to another host, first without a key, then with an
		// attacker's key. Neither may inherit the original host's authority.
		copied := moved.Config()
		copied.HostKey = nil
		if _, err := api.NewClient(logger.Discard, copied).Connect(t.Context()); err == nil {
			t.Fatal("copied token worked without the host key")
		}
		copied.TokenType = ""
		if _, err := api.NewClient(logger.Discard, copied).Connect(t.Context()); err == nil {
			t.Fatal("copied bound token worked through legacy token authentication")
		}
		other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		copied.TokenType = "DPoP"
		copied.HostKey, err = identity.New(other)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := api.NewClient(logger.Discard, copied).Connect(t.Context()); err == nil {
			t.Fatal("copied token worked with a different host key")
		}
	}
}

func TestHostBindingMustBeConfirmed(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := identity.New(private)
	if err != nil {
		t.Fatal(err)
	}
	for _, response := range []string{
		`{"access_token":"session"}`,
		`{"access_token":"session","token_type":"DPoP","dpop_jkt":"another-key"}`,
		fmt.Sprintf(`{"access_token":"","token_type":"DPoP","dpop_jkt":%q}`, key.Thumbprint()),
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			client := api.NewClient(logger.Discard, api.Config{
				Endpoint: server.URL, Token: "grant", HostKey: key, HTTPClient: server.Client(),
			})
			if _, _, err := client.Enroll(t.Context()); err == nil {
				t.Fatal("enrollment accepted missing or mismatched binding")
			}
			if _, _, err := client.Register(t.Context(), &api.AgentRegisterRequest{}); err == nil {
				t.Fatal("registration accepted missing or mismatched binding")
			}
		})
	}
}
