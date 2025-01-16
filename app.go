package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/fatih/color"
	"github.com/go-resty/resty/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Variables to store the last prices
var lastPrice, lastSellPrice float64

// Config structure to hold configuration data
type Config struct {
	TelegramToken string `json:"telegram_token"`
	ChatID        string `json:"chat_id"`
	APIURL        string `json:"api_url"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

// Load configuration from config.json
func loadConfig() Config {
	file, err := os.ReadFile("config.json")
	if err != nil {
		color.Red("Error reading config file: %s\n", err)
		os.Exit(1)
	}

	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		color.Red("Error parsing config file: %s\n", err)
		os.Exit(1)
	}

	return config
}

func main() {
	// Load configuration
	config := loadConfig()

	// Get refresh time from the user
	refreshTime := getRefreshTimeFromUser()

	// Get the addition and deduction amounts from the user
	buyAddition := getAmountFromUser("Enter the amount to add to buy price: ")
	sellDeduction := getAmountFromUser("Enter the amount to deduct from sell price: ")

	client := resty.New() // HTTP client

	// Flag to check if it's the first run
	firstRun := true

	// Infinite loop to fetch data every `refreshTime` minutes
	for {
		// Send login request
		resp, err := client.R().
			SetBody(map[string]string{
				"username": config.Username,
				"password": config.Password,
			}).
			SetHeader("Content-Type", "application/json").
			Post(config.APIURL + "/Authenticate")

		if err != nil {
			color.Red("Error in login request: %s\n", err)
			continue
		}

		// Check if the request was successful
		if resp.StatusCode() != 200 {
			color.Red("Login failed: Status Code: %d\n", resp.StatusCode())
			continue
		}

		// Extract and display AED currency information
		showAEDCurrency(resp.String(), buyAddition, sellDeduction, config.TelegramToken, config.ChatID, &firstRun)

		// Wait for the specified refresh time
		time.Sleep(time.Duration(refreshTime) * time.Minute)
	}
}

// Get refresh time from the user
func getRefreshTimeFromUser() int {
	reader := bufio.NewReader(os.Stdin)
	color.Yellow("Enter refresh time in minutes: ")
	input, _ := reader.ReadString('\n')

	// Remove extra characters and convert to integer
	refreshTime, err := strconv.Atoi(input[:len(input)-1])
	if err != nil {
		color.Red("Error converting refresh time to a number: %s\n", err)
		os.Exit(1)
	}

	if refreshTime <= 0 {
		color.Red("Refresh time must be greater than zero.\n")
		os.Exit(1)
	}

	return refreshTime
}

// Get amount from the user (for addition or deduction)
func getAmountFromUser(prompt string) float64 {
	reader := bufio.NewReader(os.Stdin)
	color.Yellow(prompt)
	input, _ := reader.ReadString('\n')

	// Remove extra characters and convert to float64
	amount, err := strconv.ParseFloat(input[:len(input)-1], 64)
	if err != nil {
		color.Red("Error converting amount to a number: %s\n", err)
		os.Exit(1)
	}

	if amount < 0 {
		color.Red("Amount cannot be negative.\n")
		os.Exit(1)
	}

	return amount
}

// Display AED currency information and send to Telegram if there are changes
func showAEDCurrency(jsonStr string, buyAddition, sellDeduction float64, telegramToken, chatID string, firstRun *bool) {
	var data map[string]interface{} // To hold JSON data
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		color.Red("Error processing JSON: %s\n", err)
		return
	}

	// Extract the currencies section
	currencies, ok := data["data"].(map[string]interface{})["currencies"].([]interface{})
	if !ok {
		color.Red("Error extracting currency data\n")
		return
	}

	// Find the AED currency
	var aedCurrency map[string]interface{}
	for _, currency := range currencies {
		curr := currency.(map[string]interface{})
		if curr["symbol"] == "AED" {
			aedCurrency = curr
			break
		}
	}

	if aedCurrency == nil {
		color.Red("AED currency not found\n")
		return
	}

	// Get current prices (swap buy and sell prices)
	currentSellPrice, _ := strconv.ParseFloat(fmt.Sprintf("%v", aedCurrency["price"]), 64)
	currentPrice, _ := strconv.ParseFloat(fmt.Sprintf("%v", aedCurrency["sellPrice"]), 64)

	// Adjust prices
	adjustedBuyPrice := currentPrice + buyAddition
	adjustedSellPrice := currentSellPrice - sellDeduction

	// Get current time in Iran (IRST)
	loc, _ := time.LoadLocation("Asia/Tehran")
	currentTime := time.Now().In(loc).Format("15:04")

	// Format the terminal message in English
	terminalMessage := fmt.Sprintf(
		"Time (IRST): %s\n\n"+
			"AED/TOMAN (Transfer) 🇦🇪\n\n"+
			"Sell: %s\n"+
			"Buy: %s\n",
		currentTime,
		formatNumber(adjustedSellPrice),
		formatNumber(adjustedBuyPrice),
	)

	// Display the terminal message
	color.Cyan("\n%s", terminalMessage)

	// Format the Telegram message in Persian
	telegramMessage := fmt.Sprintf(
		"زمان (به وقت ایران): %s\n\n"+
			"درهم/تومان (حواله) 🇦🇪\n\n"+
			"%s :فروش\n"+
			"%s :خرید\n",
		currentTime,
		formatNumber(adjustedSellPrice),
		formatNumber(adjustedBuyPrice),
	)

	// Send the Telegram message if it's the first run or prices have changed
	if *firstRun || currentPrice != lastPrice || currentSellPrice != lastSellPrice {
		if !*firstRun && (currentPrice != lastPrice || currentSellPrice != lastSellPrice) {
			// ارسال پیام تغییر قیمت
			changeMessage := fmt.Sprintf(
				"زمان (به وقت ایران): %s\n\n"+
					"تغییر قیمت! 🚨\n\n"+
					"درهم/تومان (حواله) 🇦🇪\n\n"+
					"%s :فروش %s\n"+
					"%s :خرید %s\n",
				currentTime,
				formatNumber(adjustedSellPrice), getChangeSymbol(lastSellPrice, currentSellPrice),
				formatNumber(adjustedBuyPrice), getChangeSymbol(lastPrice, currentPrice),
			)
			sendTelegramMessage(changeMessage, telegramToken, chatID)
		} else {
			// ارسال پیام اصلی (فقط در اولین اجرا)
			sendTelegramMessage(telegramMessage, telegramToken, chatID)
		}

		// به‌روزرسانی آخرین قیمت‌ها
		lastPrice = currentPrice
		lastSellPrice = currentSellPrice

		// علامت‌گذاری اولین اجرا
		if *firstRun {
			*firstRun = false
		}
	}
}

// Determine the symbol for price changes
func getChangeSymbol(oldPrice, newPrice float64) string {
	if oldPrice == 0 {
		return "" // No symbol for the first fetch
	}
	if newPrice > oldPrice {
		return "📈" // Price increased
	} else if newPrice < oldPrice {
		return "📉" // Price decreased
	}
	return "" // No change
}

// Format numbers with thousands separators
func formatNumber(num float64) string {
	p := message.NewPrinter(language.English)
	return p.Sprintf("%.0f", num) // Formats without decimal places
}

// Send a message to TelegramBot
func sendTelegramMessage(text, telegramToken, chatID string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramToken)
	payload := map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown", // Use Markdown formatting
	}

	_, err := resty.New().R().
		SetBody(payload).
		Post(url)

	if err != nil {
		color.Red("Error sending message to Telegram: %s\n", err)
	} else {
		color.Green("Message sent to Telegram successfully!\n")
	}
}
