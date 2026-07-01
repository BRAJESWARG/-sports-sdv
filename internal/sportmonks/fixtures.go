package sportmonks

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// fixturesFS holds sample SportMonks envelopes served in mock mode.
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

// fixtureFile chooses the fixture filename for an upstream path.
func fixtureFile(path string) string {
	switch {
	case strings.HasPrefix(path, "/fixtures/"): // single fixture (scorecard)
		return "fixture.json"
	case path == "/fixtures":
		return "fixtures.json"
	case strings.HasPrefix(path, "/standings/season/"):
		return "standings.json"
	default:
		// "/livescores" -> "livescores.json", "/team-rankings" -> "team-rankings.json", etc.
		return strings.TrimPrefix(path, "/") + ".json"
	}
}
