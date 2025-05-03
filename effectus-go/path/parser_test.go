package path

import (
	"testing"
)

func TestParticiplePathParser(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantNamespace  string
		wantElements   int
		wantIndices    []int    // -1 means no index
		wantStringKeys []string // "" means no string key
		wantErr        bool
	}{
		{
			name:           "simple path",
			path:           "customer.name",
			wantNamespace:  "customer",
			wantElements:   1,
			wantIndices:    []int{-1},
			wantStringKeys: []string{""},
			wantErr:        false,
		},
		{
			name:           "path with index",
			path:           "order.items[0].price",
			wantNamespace:  "order",
			wantElements:   2,
			wantIndices:    []int{0, -1},
			wantStringKeys: []string{"", ""},
			wantErr:        false,
		},
		{
			name:           "path with string key",
			path:           "order.metadata[\"status\"]",
			wantNamespace:  "order",
			wantElements:   1,
			wantIndices:    []int{-1},
			wantStringKeys: []string{"status"},
			wantErr:        false,
		},
		{
			name:           "complex path",
			path:           "customer.orders[0].items[2].name",
			wantNamespace:  "customer",
			wantElements:   3,
			wantIndices:    []int{0, 2, -1},
			wantStringKeys: []string{"", "", ""},
			wantErr:        false,
		},
		{
			name:           "empty path",
			path:           "",
			wantNamespace:  "",
			wantElements:   0,
			wantIndices:    nil,
			wantStringKeys: nil,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, elements, err := ParsePath(tt.path)

			// Check error status
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return // Don't check other expectations if we expected an error
			}

			// Check namespace
			if namespace != tt.wantNamespace {
				t.Errorf("ParsePath() namespace = %v, want %v", namespace, tt.wantNamespace)
			}

			// Check number of elements
			if len(elements) != tt.wantElements {
				t.Errorf("ParsePath() got %d elements, want %d", len(elements), tt.wantElements)
				return
			}

			// Check element indices and string keys
			for i, element := range elements {
				// Check index
				hasIndex := element.HasIndex()
				if hasIndex {
					idx, _ := element.GetIndex()
					if idx != tt.wantIndices[i] {
						t.Errorf("Element[%d] index = %d, want %d", i, idx, tt.wantIndices[i])
					}
				} else if tt.wantIndices[i] != -1 {
					t.Errorf("Element[%d] has no index, but expected %d", i, tt.wantIndices[i])
				}

				// Check string key
				hasStringKey := element.HasStringKey()
				if hasStringKey {
					key, _ := element.GetStringKey()
					if key != tt.wantStringKeys[i] {
						t.Errorf("Element[%d] string key = %s, want %s", i, key, tt.wantStringKeys[i])
					}
				} else if tt.wantStringKeys[i] != "" {
					t.Errorf("Element[%d] has no string key, but expected %s", i, tt.wantStringKeys[i])
				}
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	paths := []string{
		"customer.name",
		"order.items[0].price",
		"order.metadata[\"status\"]",
		"customer.orders[0].items[2].name",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			// Parse path
			parsedPath, err := ParseString(path)
			if err != nil {
				t.Fatalf("Failed to parse path: %v", err)
			}

			// Render back to string
			rendered := parsedPath.String()

			// Parse again
			reparsed, err := ParseString(rendered)
			if err != nil {
				t.Fatalf("Failed to reparse rendered path: %v", err)
			}

			// Compare
			if parsedPath.Namespace != reparsed.Namespace {
				t.Errorf("Namespace mismatch: %s vs %s", parsedPath.Namespace, reparsed.Namespace)
			}

			if len(parsedPath.Elements) != len(reparsed.Elements) {
				t.Errorf("Element count mismatch: %d vs %d", len(parsedPath.Elements), len(reparsed.Elements))
				return
			}

			// Compare elements
			for i, elem1 := range parsedPath.Elements {
				elem2 := reparsed.Elements[i]

				if elem1.Name != elem2.Name {
					t.Errorf("Element[%d] name mismatch: %s vs %s", i, elem1.Name, elem2.Name)
				}

				// Compare index
				if elem1.HasIndex() != elem2.HasIndex() {
					t.Errorf("Element[%d] has index mismatch", i)
				} else if elem1.HasIndex() {
					idx1, _ := elem1.GetIndex()
					idx2, _ := elem2.GetIndex()
					if idx1 != idx2 {
						t.Errorf("Element[%d] index mismatch: %d vs %d", i, idx1, idx2)
					}
				}

				// Compare string key
				if elem1.HasStringKey() != elem2.HasStringKey() {
					t.Errorf("Element[%d] has string key mismatch", i)
				} else if elem1.HasStringKey() {
					key1, _ := elem1.GetStringKey()
					key2, _ := elem2.GetStringKey()
					if key1 != key2 {
						t.Errorf("Element[%d] string key mismatch: %s vs %s", i, key1, key2)
					}
				}
			}
		})
	}
}
