package config

type AlpacaConfig struct {
	APIKey         string `json:"api_key"`
	APISecret      string `json:"api_secret"`
	TradingBaseURL string `json:"trading_base_url"` // https://paper-api.alpaca.markets
	DataBaseURL    string `json:"data_base_url"`    // https://data.alpaca.markets
	DataFeed       string `json:"data_feed"`        // iex
	ServerPort     string `json:"server_port"`      // 8080
	// Add any other configuration fields you need for Alpaca
}
