package football

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// fixturesFS holds sample SportMonks Football v3 envelopes served in mock mode.
//
//go:embed fixtures/*.json
var fixturesFS embed.FS

// mockData maps an upstream path to an embedded fixture file and returns its
// `data` payload, mirroring what get() returns for a real response.
func mockData(path string) (json.RawMessage, error) {
	name := fixtureFile(path)
	b, err := fixturesFS.ReadFile("fixtures/" + name)
	if err != nil {
		return nil, &APIError{StatusCode: 404, Endpoint: path, Message: "no mock fixture for " + path}
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("decode mock fixture %s: %w", name, err)
	}
	return env.Data, nil
}

// fixtureFile chooses the fixture filename for a v3 upstream path.
func fixtureFile(path string) string {
	switch {
	case strings.HasPrefix(path, "/livescores"):
		return "livescores.json"
	case strings.HasPrefix(path, "/fixtures/between/"), strings.HasPrefix(path, "/fixtures/date/"):
		return "fixtures.json"
	case strings.HasPrefix(path, "/fixtures/"): // single fixture
		return "fixture.json"
	case path == "/fixtures":
		return "fixtures.json"
	case strings.HasPrefix(path, "/standings/season/"):
		return "standings.json"
	default:
		return strings.TrimPrefix(path, "/") + ".json"
	}
}
