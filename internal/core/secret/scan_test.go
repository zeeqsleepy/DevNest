package secret

import "testing"

// A generated build directory is where a bundled dependency's placeholder
// credentials end up. Scanning one ordinary Next.js project produced 288
// findings inside .next and one real finding outside it, which is the report
// length that hides the finding that mattered.
func TestScanSkipsFrameworkBuildOutput(t *testing.T) {
	system := newFakeFS().
		with(".next/static/chunk.js", "const key = \""+awsKeyID+"\"\n").
		with(".nuxt/dist/app.js", "const key = \""+awsKeyID+"\"\n").
		with(".svelte-kit/output/server.js", "const key = \""+awsKeyID+"\"\n").
		with(".turbo/cache/entry.js", "const key = \""+awsKeyID+"\"\n").
		with("coverage/lcov-report/block.js", "const key = \""+awsKeyID+"\"\n").
		with("src/app.js", "const key = \""+awsKeyID+"\"\n")

	result := scan(t, system, ScanRequest{})

	if result.Count != 1 {
		t.Fatalf("found %d finding(s), want only the one outside the build output: %+v",
			result.Count, result.Findings)
	}
	if got := result.Findings[0].Path; got != "src/app.js" {
		t.Errorf("finding is in %q, want src/app.js", got)
	}
}
