package auth

import "testing"

func TestResolveSubjectFromClaims(t *testing.T) {
	tests := []struct {
		name           string
		idTokenSubject string
		userInfoSub    string
		wantSubject    string
		wantErr        bool
	}{
		{
			name:           "missing id token subject",
			idTokenSubject: "",
			userInfoSub:    "user-1",
			wantErr:        true,
		},
		{
			name:           "uses id token subject when userinfo is missing",
			idTokenSubject: "user-1",
			userInfoSub:    "",
			wantSubject:    "user-1",
		},
		{
			name:           "matching subjects",
			idTokenSubject: "user-1",
			userInfoSub:    "user-1",
			wantSubject:    "user-1",
		},
		{
			name:           "mismatched subjects",
			idTokenSubject: "user-1",
			userInfoSub:    "user-2",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSubject, err := resolveSubjectFromClaims(tt.idTokenSubject, tt.userInfoSub)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveSubjectFromClaims() error = %v, wantErr %v", err, tt.wantErr)
			}
			if gotSubject != tt.wantSubject {
				t.Fatalf("resolveSubjectFromClaims() = %q, want %q", gotSubject, tt.wantSubject)
			}
		})
	}
}
