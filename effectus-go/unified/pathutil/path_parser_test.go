package pathutil

import (
	"testing"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantNamespace string
		wantElements  []SimplePathElement
		wantError     bool
	}{
		{
			name:          "Simple path",
			path:          "customer.name",
			wantNamespace: "customer",
			wantElements: []SimplePathElement{
				{name: "name", hasIndex: false},
			},
			wantError: false,
		},
		{
			name:          "Multi-segment path",
			path:          "customer.profile.email",
			wantNamespace: "customer",
			wantElements: []SimplePathElement{
				{name: "profile", hasIndex: false},
				{name: "email", hasIndex: false},
			},
			wantError: false,
		},
		{
			name:          "With array index",
			path:          "customer.orders[0].id",
			wantNamespace: "customer",
			wantElements: []SimplePathElement{
				{name: "orders", hasIndex: true, indexVal: 0},
				{name: "id", hasIndex: false},
			},
			wantError: false,
		},
		{
			name:          "Multiple array indices",
			path:          "customer.orders[1].items[2].name",
			wantNamespace: "customer",
			wantElements: []SimplePathElement{
				{name: "orders", hasIndex: true, indexVal: 1},
				{name: "items", hasIndex: true, indexVal: 2},
				{name: "name", hasIndex: false},
			},
			wantError: false,
		},
		{
			name:          "Invalid array index",
			path:          "customer.orders[x].id",
			wantNamespace: "",
			wantElements:  nil,
			wantError:     true,
		},
		{
			name:          "Empty path",
			path:          "",
			wantNamespace: "",
			wantElements:  nil,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, elements, err := ParsePath(tt.path)

			if (err != nil) != tt.wantError {
				t.Errorf("ParsePath() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err != nil {
				return
			}

			if namespace != tt.wantNamespace {
				t.Errorf("ParsePath() namespace = %v, want %v", namespace, tt.wantNamespace)
			}

			if len(elements) != len(tt.wantElements) {
				t.Errorf("ParsePath() got %d elements, want %d", len(elements), len(tt.wantElements))
				return
			}

			for i, got := range elements {
				want := tt.wantElements[i]
				if got.Name() != want.Name() {
					t.Errorf("Element[%d].Name = %v, want %v", i, got.Name(), want.Name())
				}
				if got.HasIndex() != want.HasIndex() {
					t.Errorf("Element[%d].HasIndex = %v, want %v", i, got.HasIndex(), want.HasIndex())
				}
				if got.HasIndex() && got.Index() != want.Index() {
					t.Errorf("Element[%d].Index = %v, want %v", i, got.Index(), want.Index())
				}
			}
		})
	}
}

func TestRenderPath(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		elements  []SimplePathElement
		want      string
	}{
		{
			name:      "Simple path",
			namespace: "customer",
			elements: []SimplePathElement{
				{name: "name", hasIndex: false},
			},
			want: "customer.name",
		},
		{
			name:      "Multi-segment path",
			namespace: "customer",
			elements: []SimplePathElement{
				{name: "profile", hasIndex: false},
				{name: "email", hasIndex: false},
			},
			want: "customer.profile.email",
		},
		{
			name:      "With array index",
			namespace: "customer",
			elements: []SimplePathElement{
				{name: "orders", hasIndex: true, indexVal: 0},
				{name: "id", hasIndex: false},
			},
			want: "customer.orders[0].id",
		},
		{
			name:      "Namespace only",
			namespace: "customer",
			elements:  []SimplePathElement{},
			want:      "customer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderPath(tt.namespace, tt.elements)
			if got != tt.want {
				t.Errorf("RenderPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathRoundTrip(t *testing.T) {
	paths := []string{
		"customer.name",
		"customer.profile.email",
		"customer.orders[0].id",
		"customer.orders[1].items[2].name",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			// Parse the path
			namespace, elements, err := ParsePath(path)
			if err != nil {
				t.Fatalf("ParsePath() error = %v", err)
			}

			// Render it back to a string
			got := RenderPath(namespace, elements)

			// Compare with the original
			if got != path {
				t.Errorf("Round trip failed: got %v, want %v", got, path)
			}
		})
	}
}
