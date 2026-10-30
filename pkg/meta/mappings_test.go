package meta

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProperty_DeepClone(t *testing.T) {
	prop := NewProperty("text")
	prop.AddField("sub_field", NewProperty("text"))
	prop.Sortable = true
	prop.Aggregatable = true
	prop.Index = true
	prop.Store = true
	prop.Aggregatable = true
	prop.Highlightable = true
	prop.Analyzer = "some_nice_analyzer"
	prop.SearchAnalyzer = "search_analyzer"
	prop.Format = "2009-11-10T23:00:00Z"

	clone := prop.DeepClone()
	assert.Equal(t, prop, clone)
}

func TestMappings_RangeProperty(t *testing.T) {
	m := NewMappings()
	m.SetProperty("a", NewProperty("text"))
	m.SetProperty("b", NewProperty("keyword"))
	m.SetProperty("c", NewProperty("nested"))

	t.Run("visits all", func(t *testing.T) {
		got := map[string]string{}
		m.RangeProperty(func(field string, prop Property) bool {
			got[field] = prop.Type
			return true
		})
		assert.Equal(t, map[string]string{"a": "text", "b": "keyword", "c": "nested"}, got)
	})

	t.Run("stops early", func(t *testing.T) {
		n := 0
		m.RangeProperty(func(string, Property) bool {
			n++
			return false
		})
		assert.Equal(t, 1, n)
	})
}
