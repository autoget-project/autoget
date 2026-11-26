package mteam

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/backend/indexers"
)

func newUnauthorizedServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestMakeMultipartAPICallUpstreamError(t *testing.T) {
	ts := newUnauthorizedServer(t)

	var out map[string]any
	err := makeMultipartAPICall(ts.URL, "/x", "key", map[string]string{"id": "1"}, &out)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusBadGateway, err.Code)
}

func TestListUpstreamError(t *testing.T) {
	ts := newUnauthorizedServer(t)

	m := NewMTeam(&Config{BaseURL: ts.URL, APIKey: "key"}, MTeamTypeNormal, "", nil, nil)
	_, err := m.List(&indexers.ListRequest{})
	require.NotNil(t, err)
	assert.Equal(t, http.StatusBadGateway, err.Code)
}
