package model

import "testing"

func TestRepositoryRefURL(t *testing.T) {
	tests := []struct {
		name string
		ref  RepositoryRef
		want string
	}{
		{
			name: "base only",
			ref:  RepositoryRef{BaseURL: "https://example.invalid/online"},
			want: "https://example.invalid/online",
		},
		{
			name: "trailing slash on the base",
			ref:  RepositoryRef{BaseURL: "https://example.invalid/online/"},
			want: "https://example.invalid/online",
		},
		{
			name: "base and path",
			ref: RepositoryRef{
				BaseURL: "https://example.invalid/online",
				Path:    []string{"windows_x86", "desktop", "qt6_680"},
			},
			want: "https://example.invalid/online/windows_x86/desktop/qt6_680",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.URL(); got != tt.want {
				t.Errorf("URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRepositoryRefJSONContract pins the document shape of a reference.
func TestRepositoryRefJSONContract(t *testing.T) {
	ref := RepositoryRef{
		BaseURL: "https://example.invalid/online",
		Path:    []string{"windows_x86", "desktop"},
	}

	assertJSON(t, ref, `{"baseUrl":"https://example.invalid/online","path":["windows_x86","desktop"]}`)
}
