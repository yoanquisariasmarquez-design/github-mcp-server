//go:build e2e

package github

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/github/github-mcp-server/pkg/translations"
	"github.com/google/go-github/v73/github"
	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindClosingPullRequestsIntegration tests the FindClosingPullRequests tool with real GitHub API calls
func TestFindClosingPullRequestsIntegration(t *testing.T) {
	// This test requires a GitHub token
	token := os.Getenv("GITHUB_MCP_SERVER_E2E_TOKEN")
	if token == "" {
		t.Skip("GITHUB_MCP_SERVER_E2E_TOKEN environment variable is not set")
	}

	// Create GitHub clients
	httpClient := github.NewClient(nil).WithAuthToken(token).Client()
	gqlClient := githubv4.NewClient(httpClient)

	getGQLClient := func(ctx context.Context) (*githubv4.Client, error) {
		return gqlClient, nil
	}

	// Create the tool
	tool, handler := FindClosingPullRequests(getGQLClient, translations.NullTranslationHelper)

	// Test cases with known GitHub issues that were closed by PRs
	testCases := []struct {
		name                   string
		issues                 []string
		expectedResults        int
		expectSomeClosingPRs   bool
		expectSpecificIssue    string
		expectSpecificPRNumber int
	}{
		{
			name:                 "Single issue - VS Code well-known closed issue",
			issues:               []string{"microsoft/vscode#123456"}, // This is a made-up issue for testing
			expectedResults:      1,
			expectSomeClosingPRs: false, // We expect this to not exist or have no closing PRs
		},
		{
			name:                 "Multiple issues with mixed results",
			issues:               []string{"octocat/Hello-World#1", "microsoft/vscode#999999"},
			expectedResults:      2,
			expectSomeClosingPRs: false, // These are likely non-existent or have no closing PRs
		},
		{
			name:            "Issue from a popular repo - React",
			issues:          []string{"facebook/react#1"}, // Very first issue in React repo
			expectedResults: 1,
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create request arguments
			args := map[string]interface{}{
				"issues": tc.issues,
				"limit":  5,
			}

			// Create mock request
			request := mockCallToolRequest{
				arguments: args,
			}

			// Call the handler
			result, err := handler(ctx, request)

			if err != nil {
				t.Logf("Error calling tool: %v", err)
				// For integration tests, we might expect some errors for non-existent issues
				// Let's check if it's a reasonable error
				assert.Contains(t, err.Error(), "failed to")
				return
			}

			require.NotNil(t, result)
			assert.False(t, result.IsError, "Expected successful result")

			// Parse the response
			textContent, ok := result.Content[0].(map[string]interface{})
			if !ok {
				// Try to get as text content
				if len(result.Content) > 0 {
					if textResult, ok := result.Content[0].(string); ok {
						t.Logf("Response: %s", textResult)

						// Parse JSON response
						var response struct {
							Results []FindClosingPRsResult `json:"results"`
						}
						err := json.Unmarshal([]byte(textResult), &response)
						require.NoError(t, err, "Failed to parse JSON response")

						// Verify structure
						assert.Len(t, response.Results, tc.expectedResults, "Expected specific number of results")

						for i, result := range response.Results {
							t.Logf("Issue %d: %s", i+1, result.Issue)
							t.Logf("  Owner: %s, Repo: %s, Number: %d", result.Owner, result.Repo, result.IssueNumber)
							t.Logf("  Total closing PRs: %d", result.TotalCount)
							t.Logf("  Error: %s", result.Error)

							// Verify basic structure
							assert.NotEmpty(t, result.Issue, "Issue reference should not be empty")
							assert.NotEmpty(t, result.Owner, "Owner should not be empty")
							assert.NotEmpty(t, result.Repo, "Repo should not be empty")
							assert.Greater(t, result.IssueNumber, 0, "Issue number should be positive")

							// Log closing PRs if any
							for j, pr := range result.ClosingPullRequests {
								t.Logf("    PR %d: #%d - %s", j+1, pr.Number, pr.Title)
								t.Logf("      State: %s, Merged: %t", pr.State, pr.Merged)
								t.Logf("      URL: %s", pr.URL)
							}

							// Check for expected specific results
							if tc.expectSpecificIssue != "" && result.Issue == tc.expectSpecificIssue {
								if tc.expectSpecificPRNumber > 0 {
									found := false
									for _, pr := range result.ClosingPullRequests {
										if pr.Number == tc.expectSpecificPRNumber {
											found = true
											break
										}
									}
									assert.True(t, found, "Expected to find specific PR number")
								}
							}
						}

						return
					}
				}
				t.Fatalf("Unexpected content type: %T", result.Content[0])
			}

			t.Logf("Response content: %+v", textContent)
		})
	}
}

// mockCallToolRequest implements the mcp.CallToolRequest interface for testing
type mockCallToolRequest struct {
	arguments map[string]interface{}
}

func (m mockCallToolRequest) GetArguments() map[string]interface{} {
	return m.arguments
}
