package grok

import "time"

// SetRetryDelay overrides the retry backoff base so tests do not sleep.
func SetRetryDelay(p *Provider, d time.Duration) { p.retryDelay = d }
