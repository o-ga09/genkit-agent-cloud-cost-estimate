package intake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/firebase/genkit/go/ai"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
)

// fakeDynamoDBAPI は DynamoStore のテスト用フェイク。実際の DynamoDB には繋がず、
// PutItem / GetItem をプロセス内のマップで模倣する。
type fakeDynamoDBAPI struct {
	items map[string]map[string]types.AttributeValue
}

func newFakeDynamoDBAPI() *fakeDynamoDBAPI {
	return &fakeDynamoDBAPI{items: map[string]map[string]types.AttributeValue{}}
}

func (f *fakeDynamoDBAPI) GetItem(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	key, ok := params.Key["id"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, errors.New("id キーがありません")
	}
	item, ok := f.items[key.Value]
	if !ok {
		return &dynamodb.GetItemOutput{}, nil
	}
	return &dynamodb.GetItemOutput{Item: item}, nil
}

func (f *fakeDynamoDBAPI) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	key, ok := params.Item["id"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, errors.New("id キーがありません")
	}
	f.items[key.Value] = params.Item
	return &dynamodb.PutItemOutput{}, nil
}

func TestDynamoStore_SaveThenLoad(t *testing.T) {
	store := intake.NewDynamoStore(newFakeDynamoDBAPI(), "sessions")
	sess := &intake.Session{ID: "sess-1", Messages: []*ai.Message{
		{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("3層 Web を作りたい")}},
	}}

	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ID != "sess-1" {
		t.Errorf("ID = %q, want sess-1", got.ID)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content[0].Text != "3層 Web を作りたい" {
		t.Errorf("Messages = %+v", got.Messages)
	}
}

func TestDynamoStore_LoadUnknownReturnsNotFound(t *testing.T) {
	store := intake.NewDynamoStore(newFakeDynamoDBAPI(), "sessions")
	_, err := store.Load(context.Background(), "unknown")
	if !errors.Is(err, intake.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}
