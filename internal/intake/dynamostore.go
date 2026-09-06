package intake

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDBAPI は DynamoStore が使う *dynamodb.Client のうち必要な操作だけを
// 切り出したインターフェース。テストでは実際の DynamoDB に繋がずフェイクに差し替える。
type DynamoDBAPI interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

const (
	dynamoAttrID   = "id"
	dynamoAttrData = "data"
)

// DynamoStore はセッションを DynamoDB に保存する SessionStore（requirements.md
// の未決事項「ホスティング先」→ AWS の決定を受けて追加）。
//
// テーブルはパーティションキー "id"（S）のみを持つシンプルな形状で、
// セッション本体は MemoryStore / FileStore と同じ JSON エンコーディングを
// "data" 属性 1 つに丸ごと詰める（DynamoDB 側のスキーマをセッションの内部構造に
// 追従させない）。
type DynamoStore struct {
	client    DynamoDBAPI
	tableName string
}

// NewDynamoStore は DynamoStore を作る。client には dynamodb.NewFromConfig の
// 戻り値（*dynamodb.Client）を渡す。tableName はパーティションキー "id"（S）を
// 持つテーブル名。client を DynamoDBAPI（インターフェース）で受けることで、
// テストは実際の DynamoDB に繋がずフェイクを注入できる。
func NewDynamoStore(client DynamoDBAPI, tableName string) *DynamoStore {
	return &DynamoStore{client: client, tableName: tableName}
}

// Load は SessionStore を実装する。
func (d *DynamoStore) Load(ctx context.Context, id string) (*Session, error) {
	out, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			dynamoAttrID: &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("セッションを読み込めませんでした: %w", err)
	}
	if out.Item == nil {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	data, ok := out.Item[dynamoAttrData].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("セッションのデータ形式が不正です: %q", id)
	}
	return decodeSession([]byte(data.Value))
}

// Save は SessionStore を実装する。
func (d *DynamoStore) Save(ctx context.Context, s *Session) error {
	raw, err := encodeSession(s)
	if err != nil {
		return err
	}
	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]types.AttributeValue{
			dynamoAttrID:   &types.AttributeValueMemberS{Value: s.ID},
			dynamoAttrData: &types.AttributeValueMemberS{Value: string(raw)},
		},
	})
	if err != nil {
		return fmt.Errorf("セッションを保存できませんでした: %w", err)
	}
	return nil
}

var _ SessionStore = (*DynamoStore)(nil)
