package ai_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai"
)

type NestedStruct struct {
	InnerField string `json:"inner_field" description:"An inner field"`
	InnerVal   int    `json:"inner_val"`
}

type SampleStruct struct {
	Title       string         `json:"title" description:"The title of the item"`
	Season      int            `json:"season"`
	IsAnim      bool           `json:"is_anim"`
	Tags        []string       `json:"tags"`
	Category    string         `json:"category" enum:"movie,tv_series,bango_porn"`
	Nested      NestedStruct   `json:"nested"`
	NestedSlice []NestedStruct `json:"nested_slice"`
	Ignored     string         `json:"-"`
	Optional    string         `json:"optional,omitempty"`
}

func TestGenerateStrictJSONSchema(t *testing.T) {
	t.Parallel()

	schema, err := ai.GenerateStrictJSONSchema(SampleStruct{})
	require.NoError(t, err)

	assert.Equal(t, "object", schema.Type)
	assert.Equal(t, false, schema.AdditionalProperties, "additionalProperties must be false in strict mode")

	// Strict mode requires every exported non-ignored field, including the
	// omitempty one.
	expectedRequired := []string{
		"title",
		"season",
		"is_anim",
		"tags",
		"category",
		"nested",
		"nested_slice",
		"optional",
	}
	assert.ElementsMatch(t, expectedRequired, schema.Required)

	assert.NotContains(t, schema.Properties, "ignored", `json:"-" field must not appear in properties`)

	titleProp, ok := schema.Properties["title"]
	require.True(t, ok, "title property must exist")
	assert.Equal(t, "string", titleProp.Type)
	assert.Equal(t, "The title of the item", titleProp.Description)

	catProp, ok := schema.Properties["category"]
	require.True(t, ok, "category property must exist")
	assert.Len(t, catProp.Enum, 3)

	nestedProp, ok := schema.Properties["nested"]
	require.True(t, ok, "nested property must exist")
	assert.Equal(t, "object", nestedProp.Type)
	assert.Equal(t, false, nestedProp.AdditionalProperties)
	assert.Len(t, nestedProp.Required, 2)

	data, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestGenerateOpenAPISchema(t *testing.T) {
	t.Parallel()

	schema, err := ai.GenerateOpenAPISchema(SampleStruct{})
	require.NoError(t, err)

	assert.Equal(t, "object", schema.Type)
	assert.Nil(t, schema.AdditionalProperties, "additionalProperties must be omitted in OpenAPI mode")
	assert.NotContains(t, schema.Required, "optional", "field with omitempty must not be required in OpenAPI mode")
}

func TestGenerateSchema_Nil(t *testing.T) {
	t.Parallel()

	_, err := ai.GenerateStrictJSONSchema(nil)
	assert.Error(t, err)
}

type MapTimeStruct struct {
	Meta   map[string]string      `json:"meta"`
	Any    map[string]interface{} `json:"any"`
	When   time.Time              `json:"when"`
	Points []map[string]int       `json:"points"`
}

func TestGenerateSchema_MapsAndTime(t *testing.T) {
	t.Parallel()

	schema, err := ai.GenerateStrictJSONSchema(MapTimeStruct{})
	require.NoError(t, err)

	metaProp, ok := schema.Properties["meta"]
	require.True(t, ok, "meta property must exist")
	assert.Equal(t, "object", metaProp.Type)
	metaVal, ok := metaProp.AdditionalProperties.(ai.JSONSchema)
	require.True(t, ok, "meta additionalProperties must be a JSONSchema")
	assert.Equal(t, "string", metaVal.Type)

	anyProp, ok := schema.Properties["any"]
	require.True(t, ok, "any property must exist")
	assert.Equal(t, true, anyProp.AdditionalProperties)

	whenProp, ok := schema.Properties["when"]
	require.True(t, ok, "when property must exist")
	assert.Equal(t, "string", whenProp.Type, "time.Time must map to {type: string}")

	pointsProp, ok := schema.Properties["points"]
	require.True(t, ok, "points property must exist")
	assert.Equal(t, "array", pointsProp.Type)
	require.NotNil(t, pointsProp.Items)
	assert.NotNil(t, pointsProp.Items.AdditionalProperties, "points items must carry typed additionalProperties")
}
