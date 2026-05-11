package authgate_test

import (
	"errors"
	"testing"

	"github.com/youyo/x/internal/authgate"
)

// setDynamoDBTestEnv は AWS SDK v2 のテスト用環境変数を設定する。
//
// 開発者ローカルの SSO profile / IMDS lookup を遮断するために最低限の env を
// 注入する。SDK v2 では `AWS_PROFILE=""` や `AWS_SDK_LOAD_CONFIG=false` は
// no-op / 曖昧挙動のため含めない。
//
// 注意: `config.LoadDefaultConfig` は credential を lazy 解決するため、
// ダミーの ACCESS_KEY を渡しても実 API call は発生しない (~11ms で返る)。
func setDynamoDBTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

// TestNewDynamoDBStore_EmptyTableName は空文字列の tableName を渡すと
// ErrDynamoDBTableRequired が errors.Is 互換で返ることを確認する。
//
// spec §11 で `STORE_BACKEND=dynamodb` モードでは `DYNAMODB_TABLE_NAME` の
// 解決結果として必ず非空テーブル名が渡される想定。defaulting は CLI 層 (M24)
// の責務で、authgate 層は空文字を errors.Is で識別可能な sentinel で弾く
// 責務に徹する。
func TestNewDynamoDBStore_EmptyTableName(t *testing.T) {
	t.Parallel()

	s, err := authgate.NewDynamoDBStore("", "us-east-1")
	if err == nil {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal("NewDynamoDBStore(\"\", \"us-east-1\") expected error, got nil")
	}
	if !errors.Is(err, authgate.ErrDynamoDBTableRequired) {
		t.Fatalf("err = %v, want errors.Is ErrDynamoDBTableRequired", err)
	}
	if s != nil {
		t.Fatalf("NewDynamoDBStore returned non-nil store: %v", s)
	}
}

// TestNewDynamoDBStore_EmptyRegion は空文字列の region を渡すと
// ErrDynamoDBRegionRequired が errors.Is 互換で返ることを確認する。
//
// `AWS_REGION` の defaulting (spec §11) も CLI 層の責務。
func TestNewDynamoDBStore_EmptyRegion(t *testing.T) {
	t.Parallel()

	s, err := authgate.NewDynamoDBStore("x-test", "")
	if err == nil {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal("NewDynamoDBStore(\"x-test\", \"\") expected error, got nil")
	}
	if !errors.Is(err, authgate.ErrDynamoDBRegionRequired) {
		t.Fatalf("err = %v, want errors.Is ErrDynamoDBRegionRequired", err)
	}
	if s != nil {
		t.Fatalf("NewDynamoDBStore returned non-nil store: %v", s)
	}
}

// TestNewDynamoDBStore_ImplementsStore は正常 args で NewDynamoDBStore の
// 返り値が idproxy.Store を満たし、non-nil であることを確認する。
//
// `config.LoadDefaultConfig` は credential を lazy 解決するため、ダミー
// credential 環境で実 API call なしにコンストラクタが成功することを利用する。
// 実 DynamoDB API の動作検証は上流 (`idproxy/store/dynamodb_test.go`) と
// E2E (M24) で行うため、authgate 層では「正しく上流関数を呼んでいるか」の
// sanity check に留める。
func TestNewDynamoDBStore_ImplementsStore(t *testing.T) {
	// t.Setenv を使うため t.Parallel() は併用不可 (Go test の制約)。
	setDynamoDBTestEnv(t)

	s, err := authgate.NewDynamoDBStore("x-test", "us-east-1")
	if err != nil {
		t.Fatalf("NewDynamoDBStore failed: %v", err)
	}
	if s == nil {
		t.Fatal("NewDynamoDBStore returned nil store")
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	// コンパイル時に idproxy.Store interface を満たすことを検証する補助。
	_ = idproxyStoreCompileCheck(s)
}
