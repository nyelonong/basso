// Proposal POST retry: free and stealth gateways intermittently return 429/5xx
// or drop connections; a bounded exponential-backoff retry hides the transient
// failures while the caller's context still bounds total wait time.
package ai

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
)

const (
	// proposalAttempts is the per-request attempt budget, including the first.
	proposalAttempts = 3
)

// proposalBackoffBase seeds exponential backoff between attempts; tests
// compress it.
var proposalBackoffBase = 500 * time.Millisecond

type attemptResult struct {
	statusCode int
	body       []byte
}

func (r attemptResult) ok() bool {
	return r.statusCode >= http.StatusOK && r.statusCode < http.StatusMultipleChoices
}

// retryableStatus classifies gateway-transient responses worth another
// attempt; anything else (auth, validation, not found) is definitive.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// postProposal runs up to proposalAttempts POST attempts produced by newRequest,
// backing off exponentially with jitter between them. It returns either an
// attempt whose status is definitive (non-retryable) or the last failure;
// every exit path honors ctx cancellation.
func postProposal(
	ctx context.Context,
	client *http.Client,
	newRequest func(context.Context) (*http.Request, error),
) (attemptResult, error) {
	backoff := proposalBackoffBase
	var lastResult attemptResult
	var lastErr error

	for attempt := 0; attempt < proposalAttempts; attempt++ {
		request, err := newRequest(ctx)
		if err != nil {
			return attemptResult{}, fmt.Errorf("create request: %w", err)
		}
		response, err := client.Do(request)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return attemptResult{}, ctxErr
			}
			lastErr = fmt.Errorf("request failed: %w", err)
		} else {
			body, readErr := readBoundedResponse(response)
			if readErr != nil {
				return attemptResult{}, readErr
			}
			result := attemptResult{statusCode: response.StatusCode, body: body}
			if !retryableStatus(result.statusCode) {
				return result, nil
			}
			lastResult = result
			lastErr = fmt.Errorf("unexpected HTTP status %d", result.statusCode)
		}

		if attempt == proposalAttempts-1 {
			break
		}
		timer := time.NewTimer(backoff + jitter(backoff))
		select {
		case <-ctx.Done():
			timer.Stop()
			return attemptResult{}, ctx.Err()
		case <-timer.C:
			backoff *= 2
		}
	}
	return lastResult, lastErr
}

// jitter returns a random duration in [0, d/2) so repeated retries do not
// synchronize against a struggling upstream.
func jitter(d time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(d)/2 + 1))
}
