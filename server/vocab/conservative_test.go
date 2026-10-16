package vocab

import (
	"testing"

	"speektome-server/db"
)

func TestConservativeNormalizerPreservesOrdinarySpeechAndFormatting(t *testing.T) {
	terms := []db.VocabularyTerm{{Canonical: "PyTorch"}, {Canonical: "Docker"}, {Canonical: "OpenID"}, {Canonical: "FastAPI"}, {Canonical: "C++"}, {Canonical: "C#"}, {Canonical: "PostgreSQL"}}
	n, err := NewConservativeNormalizer(terms, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want string }{
		{"He carried the torch near the old docks before I opened the door.", "He carried the torch near the old docks before I opened the door."},
		{"Use\t(fast api),\npy torch and postgre sql!", "Use\t(FastAPI),\nPyTorch and PostgreSQL!"},
		{"Use c plus plus and c sharp.", "Use C++ and C#."},
		{"Use fast. API without crossing punctuation.", "Use fast. API without crossing punctuation."},
		{"An island of redisisland and dockers.", "An island of redisisland and dockers."},
	} {
		got, _ := n.Normalize(tc.in)
		if got != tc.want {
			t.Errorf("%q => %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestConservativeNormalizerRetainsCompleteMultilingualTerms(t *testing.T) {
	n, err := NewConservativeNormalizer([]db.VocabularyTerm{{Canonical: "東京 API"}, {Canonical: "Café"}}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := n.Normalize("api caf café 東京 api")
	if got != "api caf Café 東京 API" {
		t.Fatal(got)
	}
}

func TestConservativeAliasesAreExplicitAndVocabularyScoped(t *testing.T) {
	terms := []db.VocabularyTerm{{Canonical: "Grafana"}}
	n, _ := NewConservativeNormalizer(terms, nil, false)
	got, _ := n.Normalize("graphana")
	if got != "graphana" {
		t.Fatal(got)
	}
	n, err := NewConservativeNormalizer(terms, map[string]string{"graphana": "Grafana"}, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = n.Normalize("graphana")
	if got != "Grafana" {
		t.Fatal(got)
	}
	if _, err = NewConservativeNormalizer(terms, map[string]string{"torch": "PyTorch"}, false); err == nil {
		t.Fatal("accepted unknown alias target")
	}
}

func TestGuardedTypoMatchingRejectsShortAndAmbiguousCandidates(t *testing.T) {
	terms := []db.VocabularyTerm{{Canonical: "Kubernetes"}, {Canonical: "Kubernetex"}, {Canonical: "Docker"}, {Canonical: "OpenID"}}
	n, _ := NewConservativeNormalizer(terms, nil, true)
	for _, tc := range []struct{ in, want string }{
		{"kubernets", "Kubernetes"},
		{"kubernetez", "kubernetez"},
		{"docks opened", "docks opened"},
	} {
		got, _ := n.Normalize(tc.in)
		if got != tc.want {
			t.Errorf("%q => %q, want %q", tc.in, got, tc.want)
		}
	}
}
