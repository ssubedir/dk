package draftkings

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/coder/websocket"
	"github.com/ssubedir/dk/backend/internal/domain"
	"github.com/vmihailenco/msgpack/v5"
)

const draftKingsWebSocketURL = "wss://sportsbook-ws-ca-on.draftkings.com/websocket?format=msgpack&locale=en"
const socketSubscribeTimeout = 15 * time.Second

var errUnsupportedDelta = errors.New("unsupported DraftKings WebSocket change; REST resync required")
var socketSensitiveValue = regexp.MustCompile(`(?i)\b(?:bearer\s+|(?:access[_-]?token|refresh[_-]?token|jwt|token|authorization|cookie|api[_-]?key|secret|session[_-]?id)\s*[:=]\s*["']?)[^\s,;"']+`)

type liveSelectionUpdate = domain.SelectionUpdate

// SocketSink owns cache, publication, and recovery policy. The DraftKings
// adapter only translates the upstream protocol into normalized callbacks.
type SocketSink interface {
	SetConnected(bool)
	OnSelection(domain.SelectionUpdate, time.Time)
	OnSelectionRemoved(string)
	RequestResync()
}

type SocketClient struct {
	Config DraftKingsConfig
	Sink   SocketSink
	url    string // Empty uses the production DraftKings socket.
}

type draftKingsSocketFrame struct {
	id                  string
	kind                string
	failureDetail       string
	selections          []liveSelectionUpdate
	removedSelectionIDs []string
	requiresResync      bool
}

func (config DraftKingsConfig) subscriptionRequest(id string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "subscribe",
		"id":      id,
		"params": map[string]any{
			"entity": "events",
			"queryParams": map[string]any{
				"query":          fmt.Sprintf("$filter=leagueId eq '%s' AND clientMetadata/Subcategories/any(s: s/Id eq '%s') and tags/any(t: t eq 'OSB')", config.LeagueID, config.SubcategoryID),
				"includeMarkets": fmt.Sprintf("$filter=clientMetadata/subCategoryId eq '%s' AND tags/all(t: t ne 'SportcastBetBuilder') and tags/any(t: t eq 'OSB')", config.SubcategoryID),
				"initialData":    false,
				"projection":     "sportsbook",
				"locale":         "en",
			},
			"forwardedHeaders": map[string]any{},
			"clientMetadata": map[string]string{
				"feature":          "league",
				"X-Client-Name":    "web",
				"X-Client-Version": "2638.5.1.4",
			},
			"jwt":      "default-token",
			"siteName": "dkcaon",
		},
	}
}

func newSubscriptionID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

func (client *SocketClient) Run(ctx context.Context) {
	endpoint := client.url
	if endpoint == "" {
		endpoint = draftKingsWebSocketURL
	}
	delay := time.Second
	reconnect := false
	for ctx.Err() == nil {
		started := time.Now()
		subscribed, err := client.session(ctx, endpoint, reconnect)
		reconnect = true
		if ctx.Err() != nil {
			return
		}
		log.Printf("DraftKings WebSocket connection ended (league=%s, subscribed=%t, duration=%s): %s; reconnecting in %s",
			client.Config.LeagueName, subscribed, time.Since(started).Round(time.Millisecond), socketErrorDescription(err), delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if subscribed && time.Since(started) >= 30*time.Second {
			delay = time.Second
		} else {
			delay = min(delay*2, 30*time.Second)
		}
	}
}

func socketErrorDescription(err error) string {
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		return fmt.Sprintf("close_code=%d close_reason=%q", closeErr.Code, SafeSocketReason(closeErr.Reason))
	}
	if err == nil {
		return "connection ended without an error"
	}
	return SafeSocketReason(err.Error())
}

func SafeSocketReason(reason string) string {
	reason = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, reason)
	reason = socketSensitiveValue.ReplaceAllString(reason, "[redacted]")
	reason = strings.Join(strings.Fields(reason), " ")
	characters := []rune(reason)
	if len(characters) > 180 {
		reason = string(characters[:180]) + "…"
	}
	return reason
}

func subscriptionFailureDetail(payload any) string {
	if text, ok := payload.(string); ok && text != "" {
		return "reason=" + fmt.Sprintf("%q", SafeSocketReason(text))
	}
	if fields, ok := payload.(map[string]any); ok {
		var parts []string
		if code, exists := fields["code"]; exists {
			if text, ok := code.(string); ok {
				parts = append(parts, "code="+fmt.Sprintf("%q", SafeSocketReason(text)))
			} else if number, ok := numberAsFloat(code); ok {
				parts = append(parts, fmt.Sprintf("code=%g", number))
			}
		}
		for _, key := range []string{"message", "reason", "description", "error"} {
			if text, ok := fields[key].(string); ok && text != "" {
				parts = append(parts, "reason="+fmt.Sprintf("%q", SafeSocketReason(text)))
				break
			}
			if nested, ok := fields[key].(map[string]any); ok {
				if detail := subscriptionFailureDetail(nested); detail != "reason=not provided" {
					parts = append(parts, detail)
					break
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	if entries, ok := payload.([]any); ok {
		for _, entry := range entries {
			if detail := subscriptionFailureDetail(entry); detail != "reason=not provided" {
				return detail
			}
		}
	}
	return "reason=not provided"
}

func socketHandshakeFailure(response *http.Response) string {
	result := response.Status
	if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
		result += " retry_after=" + fmt.Sprintf("%q", SafeSocketReason(retryAfter))
	}
	if response.Body != nil && strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "json") {
		var payload map[string]any
		if json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&payload) == nil {
			if detail := subscriptionFailureDetail(payload); detail != "reason=not provided" {
				result += " " + detail
			}
		}
	}
	return result
}

// A connected socket can still miss an update without reporting an error.
// Reuse the serialized recovery worker so REST never blocks socket reads.
func (client *SocketClient) session(ctx context.Context, endpoint string, resyncOnSubscribe bool) (bool, error) {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	defer transport.CloseIdleConnections()
	headers := http.Header{}
	headers.Set("Origin", "https://sportsbook.draftkings.com")
	headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36")
	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}, HTTPHeader: headers})
	if err != nil {
		if response != nil {
			return false, fmt.Errorf("connect: upstream handshake returned %s: %w", socketHandshakeFailure(response), err)
		}
		return false, fmt.Errorf("connect: %w", err)
	}
	defer conn.CloseNow()
	defer client.Sink.SetConnected(false)
	conn.SetReadLimit(20 << 20)
	var ackTimedOut atomic.Bool
	ackTimeout := time.AfterFunc(socketSubscribeTimeout, func() {
		ackTimedOut.Store(true)
		_ = conn.CloseNow()
	})
	defer ackTimeout.Stop()
	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	pingFailures := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				deadline, cancel := context.WithTimeout(pingCtx, 10*time.Second)
				err := conn.Ping(deadline)
				cancel()
				if err != nil {
					select {
					case pingFailures <- err:
					default:
					}
					_ = conn.CloseNow()
					return
				}
			}
		}
	}()
	id, err := newSubscriptionID()
	if err != nil {
		return false, fmt.Errorf("create subscription ID: %w", err)
	}
	payload, err := msgpack.Marshal(client.Config.subscriptionRequest(id))
	if err != nil {
		return false, fmt.Errorf("encode subscription: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, payload); err != nil {
		return false, fmt.Errorf("send subscription: %w", err)
	}

	subscribed := false
	lastUnsupportedLog := time.Time{}
	for {
		messageType, raw, err := conn.Read(ctx)
		observedAt := time.Now()
		if err != nil {
			if !subscribed && ackTimedOut.Load() {
				return false, fmt.Errorf("subscription acknowledgement timed out after %s: %w", socketSubscribeTimeout, err)
			}
			select {
			case pingErr := <-pingFailures:
				return subscribed, fmt.Errorf("ping failed: %w", pingErr)
			default:
			}
			return subscribed, fmt.Errorf("read upstream frame: %w", err)
		}
		if messageType != websocket.MessageBinary {
			return subscribed, fmt.Errorf("unexpected WebSocket message type %s", messageType)
		}
		frame, err := decodeDraftKingsSocketFrame(raw)
		if err != nil {
			if subscribed {
				if time.Since(lastUnsupportedLog) >= 30*time.Second {
					log.Printf("DraftKings WebSocket frame requires REST resync: %v (repeated frame logs suppressed for 30s)", err)
					lastUnsupportedLog = time.Now()
				}
				client.Sink.RequestResync()
				continue
			}
			return false, fmt.Errorf("decode subscription response: %w", err)
		}
		if frame.id != id && !(frame.id == "" && (frame.kind == "error" || frame.kind == "unsubscribed")) {
			continue
		}
		switch frame.kind {
		case "subscribed":
			if !subscribed {
				subscribed = true
				ackTimeout.Stop()
				client.Sink.SetConnected(true)
				log.Printf("DraftKings WebSocket subscribed to %s game lines (reconnected=%t)", client.Config.LeagueName, resyncOnSubscribe)
				if resyncOnSubscribe {
					client.Sink.RequestResync() // Recover changes missed while the socket was disconnected.
				}
			}
		case "update":
			if !subscribed {
				client.Sink.RequestResync()
				continue
			}
			for _, selection := range frame.selections {
				client.Sink.OnSelection(selection, observedAt)
			}
			for _, removedID := range frame.removedSelectionIDs {
				client.Sink.OnSelectionRemoved(removedID)
			}
			if frame.requiresResync {
				client.Sink.RequestResync()
			}
		case "error", "unsubscribed":
			return subscribed, fmt.Errorf("subscription %s: %s", frame.kind, frame.failureDetail)
		}
	}
}
