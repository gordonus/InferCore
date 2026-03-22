// Package bedrock implements AWS Bedrock inference via the Converse API (AWS SDK v2).
package bedrock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
	"github.com/infercore/infercore/internal/upstream"
)

type Adapter struct {
	cfg    config.BackendConfig
	region string
}

func New(cfg config.BackendConfig) *Adapter {
	return &Adapter{
		cfg:    cfg,
		region: strings.TrimSpace(cfg.AWSRegion),
	}
}

func (a *Adapter) Name() string {
	return a.cfg.Name
}

func (a *Adapter) modelID() string {
	return strings.TrimSpace(a.cfg.DefaultModel)
}

func (a *Adapter) Invoke(ctx context.Context, req types.BackendRequest) (types.BackendResponse, error) {
	text, _ := req.Input["text"].(string)
	if text == "" {
		text = "No text provided."
	}
	modelID := a.modelID()
	if modelID == "" {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "bedrock backend requires default_model (model ID)")
	}
	if a.region == "" {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "bedrock backend requires aws_region")
	}

	if req.Options.Stream {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, "bedrock adapter: streaming not implemented yet; disable options.stream")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(a.region))
	if err != nil {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, fmt.Sprintf("aws config: %v", err))
	}
	client := bedrockruntime.NewFromConfig(awsCfg)

	t0 := time.Now()
	out, err := client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(modelID),
		Messages: []brtypes.Message{
			{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: text},
				},
			},
		},
	})
	if err != nil {
		return types.BackendResponse{}, upstream.New(upstream.KindBackendError, err.Error())
	}

	outText := extractOutputText(out)
	if outText == "" {
		outText = "[bedrock] empty response"
	}

	lat := time.Since(t0).Milliseconds()
	return types.BackendResponse{
		Output: map[string]any{
			"text":    outText,
			"backend": a.cfg.Name,
		},
		Timing: &types.BackendTiming{
			TTFTMs:              lat,
			CompletionLatencyMs: lat,
			Streamed:            false,
		},
	}, nil
}

func extractOutputText(out *bedrockruntime.ConverseOutput) string {
	if out == nil || out.Output == nil {
		return ""
	}
	var msg *brtypes.Message
	switch v := out.Output.(type) {
	case *brtypes.ConverseOutputMemberMessage:
		msg = &v.Value
	default:
		return ""
	}
	if msg == nil {
		return ""
	}
	var b strings.Builder
	for _, block := range msg.Content {
		switch v := block.(type) {
		case *brtypes.ContentBlockMemberText:
			b.WriteString(v.Value)
		}
	}
	return b.String()
}

func (a *Adapter) Health(ctx context.Context) error {
	if a.region == "" {
		return upstream.New(upstream.KindBackendUnhealthy, "bedrock backend requires aws_region")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(a.region))
	if err != nil {
		return upstream.New(upstream.KindBackendUnhealthy, err.Error())
	}
	client := bedrock.NewFromConfig(awsCfg)
	_, err = client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return upstream.New(upstream.KindBackendUnhealthy, err.Error())
	}
	return nil
}

func (a *Adapter) Metadata() types.BackendMetadata {
	return types.BackendMetadata{
		Name:           a.cfg.Name,
		Type:           a.cfg.Type,
		Capabilities:   a.cfg.Capabilities,
		CostUnit:       a.cfg.Cost.Unit,
		MaxConcurrency: a.cfg.MaxConcurrency,
	}
}
