package exchange

import "testing"

func TestExchangeFactory_CreateMock(t *testing.T) {
	factory := NewExchangeFactory()
	ex, err := factory.Create("mock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ex.Name() != "mock" {
		t.Errorf("expected 'mock', got '%s'", ex.Name())
	}
}

func TestExchangeFactory_CreateUnknown(t *testing.T) {
	factory := NewExchangeFactory()
	_, err := factory.Create("unknown_exchange")
	if err == nil {
		t.Error("expected error for unknown exchange")
	}
}

func TestExchangeFactory_Register(t *testing.T) {
	factory := NewExchangeFactory()
	customExchange := &MockExchange{}
	factory.Register("custom", customExchange)

	ex, err := factory.Create("custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ex != customExchange {
		t.Error("expected registered exchange to be returned")
	}
}

func TestExchangeFactory_SetAndGetConfig(t *testing.T) {
	factory := NewExchangeFactory()
	config := ExchangeConfig{APIKey: "test_key", APISecret: "test_secret"}
	factory.SetConfig("mock", config)

	retrieved := factory.GetConfig("mock")
	if retrieved.APIKey != "test_key" {
		t.Errorf("expected 'test_key', got '%s'", retrieved.APIKey)
	}
}
