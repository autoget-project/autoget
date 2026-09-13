package stage2enricher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActorStore_ReadWriteAndMerge(t *testing.T) {
	t.Parallel()

	actorFile := filepath.Join(t.TempDir(), "actor.json")
	store := NewActorStore(actorFile, "", nil)

	// Initially empty.
	_, found, err := store.FindDir([]string{"Yui Hatano"})
	require.NoError(t, err)
	assert.False(t, found)

	// Add actor.
	chosen, err := store.AddActorAlias("波多野结衣", []string{"Yui Hatano", "波多野结衣", "波多野結衣"})
	require.NoError(t, err)
	assert.Equal(t, "波多野结衣", chosen)

	// FindDir by alias.
	dir, found, err := store.FindDir([]string{"Yui Hatano"})
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "波多野结衣", dir)

	// Merge a new alias into the existing directory.
	chosen2, err := store.AddActorAlias("波多野_other", []string{"波多野結衣", "Hatano Yui New"})
	require.NoError(t, err)
	assert.Equal(t, "波多野结衣", chosen2)

	// Verify the on-disk JSON format.
	data, err := os.ReadFile(actorFile)
	require.NoError(t, err)
	var diskMap map[string][]string
	require.NoError(t, json.Unmarshal(data, &diskMap))
	assert.GreaterOrEqual(t, len(diskMap["波多野结衣"]), 4, "merged aliases must be persisted")
}

func TestActorStore_FlockConcurrency(t *testing.T) {
	t.Parallel()

	actorFile := filepath.Join(t.TempDir(), "actor.json")
	store := NewActorStore(actorFile, "", nil)

	var wg sync.WaitGroup
	workers := 10
	iterations := 5

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			name := "ActorA"
			if workerID%2 == 1 {
				name = "ActorB"
			}
			alias := name + "_alias"
			for i := 0; i < iterations; i++ {
				// Non-fatal assertions are safe from multiple goroutines.
				_, err := store.AddActorAlias(name, []string{alias})
				assert.NoError(t, err)
			}
		}(w)
	}
	wg.Wait()

	// Verify both actors exist and the file is still valid JSON.
	data, err := os.ReadFile(actorFile)
	require.NoError(t, err)
	var res map[string][]string
	require.NoError(t, json.Unmarshal(data, &res), "concurrent writes must not corrupt the JSON")
	assert.Contains(t, res, "ActorA")
	assert.Contains(t, res, "ActorB")
}

func TestActorStore_ParseArchived1Json(t *testing.T) {
	t.Parallel()

	fixture := []byte(`{
		"status": "ok",
		"solution": {
			"response": "<html><body><div class=\"actor-box\"><a href=\"#\" title=\"三上悠亞, 鬼头桃菜\">三上悠亚</a></div></body></html>"
		}
	}`)

	aliases := ParseJavDBResponse(fixture, "三上悠亚")
	assert.NotEmpty(t, aliases)

	// 三上悠亜 and 鬼头桃菜 must be extracted from title="三上悠亞, 鬼头桃菜".
	assert.Contains(t, aliases, "三上悠亞")
	assert.Contains(t, aliases, "鬼头桃菜")
}
