package data

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestToYAMLKeepsKeyOrder(t *testing.T) {
	system := newFakeFS().with("config.json", `{"zeta":1,"alpha":{"b":2},"list":[1,2]}`)

	result, err := ToYAML(system, ConvertRequest{Request: file("config.json")})
	if err != nil {
		t.Fatalf("ToYAML: %v", err)
	}

	zeta := strings.Index(result.Output, "zeta")
	alpha := strings.Index(result.Output, "alpha")
	if zeta < 0 || alpha < 0 || zeta > alpha {
		t.Errorf("output =\n%s\nwant zeta before alpha, as written", result.Output)
	}
	if result.From != FormatJSON || result.To != FormatYAML {
		t.Errorf("result = %+v, want a json to yaml conversion", result)
	}
}

func TestToYAMLRefusesBrokenJSON(t *testing.T) {
	system := newFakeFS().with("broken.json", `{"a":}`)

	_, err := ToYAML(system, ConvertRequest{Request: file("broken.json")})
	assertCode(t, err, errors.CodeParse)
}

func TestToJSONConvertsOneDocument(t *testing.T) {
	system := newFakeFS().with("config.yaml", "zeta: 1\nalpha:\n  b: 2\nlist:\n  - a\n  - b\n")

	result, err := ToJSON(system, ConvertRequest{Request: file("config.yaml")})
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	if !strings.HasPrefix(result.Output, "{") {
		t.Errorf("output =\n%s\nwant a JSON object", result.Output)
	}
	if strings.Index(result.Output, "zeta") > strings.Index(result.Output, "alpha") {
		t.Errorf("output =\n%s\nwant the source key order kept", result.Output)
	}
	if result.Documents != 1 {
		t.Errorf("documents = %d, want 1", result.Documents)
	}
}

func TestToJSONTurnsSeveralDocumentsIntoAnArray(t *testing.T) {
	system := newFakeFS().with("manifest.yaml",
		"# a comment\nkind: Service\n---\nkind: Deployment\n---\n# nothing here\n")

	result, err := ToJSON(system, ConvertRequest{Request: file("manifest.yaml")})
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	if result.Documents != 2 {
		t.Errorf("documents = %d, want the empty one skipped", result.Documents)
	}
	if !strings.HasPrefix(result.Output, "[") {
		t.Errorf("output =\n%s\nwant an array", result.Output)
	}
	if !strings.Contains(result.Output, "Service") || !strings.Contains(result.Output, "Deployment") {
		t.Errorf("output =\n%s\nwant both documents", result.Output)
	}
}

func TestToJSONResolvesAnchors(t *testing.T) {
	system := newFakeFS().with("anchors.yaml",
		"defaults: &defaults\n  retries: 3\nservice:\n  <<: *defaults\n  name: api\n")

	result, err := ToJSON(system, ConvertRequest{Request: file("anchors.yaml")})
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	if !strings.Contains(result.Output, `"retries": 3`) {
		t.Errorf("output =\n%s\nwant the merged anchor resolved", result.Output)
	}
}

func TestToJSONReportsBrokenYAML(t *testing.T) {
	system := newFakeFS().with("broken.yaml", "a:\n\t- 1\n")

	_, err := ToJSON(system, ConvertRequest{Request: file("broken.yaml")})
	assertCode(t, err, errors.CodeParse)

	if text := message(t, err); !strings.Contains(text, "line") {
		t.Errorf("message = %q, want a position in it", text)
	}
}

func TestToJSONRejectsAFileOfComments(t *testing.T) {
	system := newFakeFS().with("comments.yaml", "# nothing\n# at all\n")

	_, err := ToJSON(system, ConvertRequest{Request: file("comments.yaml")})
	assertCode(t, err, errors.CodeParse)
}

func TestConversionsRoundTrip(t *testing.T) {
	const original = `{"name":"devnest","ports":[80,443],"nested":{"on":true}}`
	system := newFakeFS().with("config.json", original)

	toYAML, err := ToYAML(system, ConvertRequest{Request: file("config.json")})
	if err != nil {
		t.Fatalf("ToYAML: %v", err)
	}

	back, err := ToJSON(system, ConvertRequest{Request: piped(toYAML.Output)})
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	minified, err := Minify(system, MinifyRequest{Request: piped(back.Output)})
	if err != nil {
		t.Fatalf("Minify: %v", err)
	}
	if minified.Output != original {
		t.Errorf("round trip = %s, want %s", minified.Output, original)
	}
}
