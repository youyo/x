package authgate

import (
	"errors"
	"fmt"

	"github.com/youyo/idproxy"
	"github.com/youyo/idproxy/store"
)

// ErrDynamoDBTableRequired は [NewDynamoDBStore] に空文字を渡した場合に返る
// エラーである。errors.Is で判定すること。
//
// spec §11 の `STORE_BACKEND=dynamodb` モードでは `DYNAMODB_TABLE_NAME` の
// 解決結果として必ず非空テーブル名が渡される想定。defaulting は CLI 層 (M24)
// の責務で、authgate 層は空文字を errors.Is で識別可能な sentinel で弾く
// 責務に徹する。
var ErrDynamoDBTableRequired = errors.New("authgate: dynamodb table name is required")

// ErrDynamoDBRegionRequired は [NewDynamoDBStore] に空の region を渡した場合に
// 返るエラーである。errors.Is で判定すること。
//
// spec §11 の `AWS_REGION` (例: `ap-northeast-1`, `us-east-1`) の defaulting
// は CLI 層 (M24) の責務で、authgate 層は空文字を errors.Is で識別可能な
// sentinel で弾く責務に徹する。
var ErrDynamoDBRegionRequired = errors.New("authgate: dynamodb region is required")

// NewDynamoDBStore は spec §11 の `DYNAMODB_TABLE_NAME` / `AWS_REGION` を
// 受け取り、[github.com/youyo/idproxy/store.NewDynamoDBStore] を呼んで
// idproxy.Store の DynamoDB 実装を返す薄いラッパーである。
//
// 用途: spec §11 `STORE_BACKEND=dynamodb` モード。Lambda マルチコンテナ環境
// でのコンテナ間状態共有 / コールドスタート跨ぎでの状態永続化を想定する。
//
// 引数:
//
//   - tableName: DynamoDB テーブル名 (例: `x-mcp-idproxy`)。spec §11 の
//     `DYNAMODB_TABLE_NAME` を CLI 層 (M24) で解決した値が渡される想定。
//   - region: AWS リージョン (例: `ap-northeast-1`, `us-east-1`)。spec §11 の
//     `AWS_REGION` を CLI 層 (M24) で解決した値が渡される想定。
//
// 動作:
//
//   - 空 tableName → [ErrDynamoDBTableRequired]
//   - 空 region → [ErrDynamoDBRegionRequired]
//   - 上流 `idproxy/store.NewDynamoDBStore` の失敗 (例: `config.LoadDefaultConfig`
//     のエラー) → fmt.Errorf で wrap
//
// AWS SDK v2 の credential 解決は lazy (最初の API call 時) のため、コンストラクタ
// は AWS account / credential 無しでも成功する。実 DynamoDB API の到達性
// 検証は本関数では行わない (logvalet 統合例と同等)。
//
// 戻り値:
//
//   - 成功時: idproxy.Store interface を満たす値。利用者は不要になった時点で
//     [idproxy.Store.Close] を呼び出す責務を負う。Close は冪等で、以降の
//     操作は internal sentinel で reject される。
//   - 失敗時: 上記いずれかのエラー。
//
// TTL は idproxy/store/dynamodb 側が DynamoDB ネイティブ TTL + Get 時の二重
// チェックで実装済みのため、本関数では扱わない。session / authcode の Get は
// ConsistentRead=true 固定 (上流実装)。
//
// テーブルスキーマ:
//
//   - パーティションキー: `pk` (String) — 例: `session:<id>`, `authcode:<code>`
//   - 属性: `data` (String, JSON), `ttl` (Number, Unix epoch 秒)
//   - DynamoDB TTL 属性: `ttl` を指定 (上流 dynamodb_store の前提)
//
// テーブル作成は本マイルストーンの範囲外 (Lambda デプロイ手順 §M25 で扱う)。
func NewDynamoDBStore(tableName, region string) (idproxy.Store, error) {
	if tableName == "" {
		return nil, ErrDynamoDBTableRequired
	}
	if region == "" {
		return nil, ErrDynamoDBRegionRequired
	}

	s, err := store.NewDynamoDBStore(tableName, region)
	if err != nil {
		return nil, fmt.Errorf("authgate: open dynamodb store: %w", err)
	}
	return s, nil
}
