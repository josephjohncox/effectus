package main

import (
	"embed"
	"fmt"
	"strings"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
)

//go:embed rules/review.eff rules/review.effx
var rules embed.FS

const tutorialResolverID = "example/language-tutorial/v1"

func tutorialBundle(dialect, diagnostic string) (*bundle.SourceBundle, error) {
	if dialect != "eff" && dialect != "effx" {
		return nil, fmt.Errorf("dialect must be eff or effx")
	}
	path := "rules/review." + dialect
	data, err := rules.ReadFile(path)
	if err != nil {
		return nil, err
	}
	source := string(data)
	switch diagnostic {
	case "":
	case "unknown-fact":
		source = strings.ReplaceAll(source, "order.total", "order.missing")
	case "type-mismatch":
		source = strings.ReplaceAll(source, "orderId: order.id", "orderId: 42")
	case "future-binding":
		producer := `ticket = RequestManualReview(orderId: order.id, reason: "value_or_risk")`
		consumer := `RecordReview(orderId: order.id, ticket: $ticket)`
		source = strings.Replace(source, producer+"\n    "+consumer, consumer+"\n    "+producer, 1)
	default:
		return nil, fmt.Errorf("diagnostic must be unknown-fact, type-mismatch, or future-binding")
	}
	descriptors := make(map[string]invocation.Descriptor)
	for _, verb := range []string{"RequestManualReview", "RecordReview"} {
		descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{
			Type: invocation.DescriptorEmbedded, ResolverID: tutorialResolverID, Reference: verb,
		})
		if err != nil {
			return nil, err
		}
		descriptors[verb] = descriptor
	}
	return bundle.New(bundle.Spec{
		Name: "language-tutorial", Version: "1.0.0",
		Sources: []bundle.Source{{Path: path, Content: source}},
		Environment: ir.Environment{
			Facts: map[string]string{"order.id": "string", "order.total": "float", "order.risk_score": "int"},
			Verbs: map[string]ir.VerbContract{
				"RequestManualReview": {Arguments: map[string]string{"orderId": "string", "reason": "string"}, RequiredArgs: []string{"orderId", "reason"}, ResultType: "string"},
				"RecordReview":        {Arguments: map[string]string{"orderId": "string", "ticket": "string"}, RequiredArgs: []string{"orderId", "ticket"}, ResultType: "void"},
			},
		},
		Executors: descriptors,
	})
}
