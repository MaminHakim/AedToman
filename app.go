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
var lastPrice, lastSellPrice, lastAlipayPrice float64

// Config structure to hold configuration data
type Config struct {
	TelegramToken         string   `json:"telegram_token"`
	ChatID                string   `json:"chat_id"`
	APIURL                string   `json:"api_url"`
	Username              string   `json:"username"`
	Password              string   `json:"password"`
	WhatsAppList          []string `json:"whatsapp_list"`           // WhatsApp numbers list
	WhatsAppBroadcastList string   `json:"whatsapp_broadcast_list"` // WhatsApp broadcast channel address
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

		// Extract and display AED and Alipay currency information
		showCurrencies(resp.String(), buyAddition, sellDeduction, config.TelegramToken, config.ChatID, &firstRun, config)

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

// Display AED and Alipay currency information and send to Telegram/WhatsApp if there are changes
func showCurrencies(jsonStr string, buyAddition, sellDeduction float64, telegramToken, chatID string, firstRun *bool, config Config) {
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

	// Find the AED and Alipay currencies
	var aedCurrency, alipayCurrency map[string]interface{}
	for _, currency := range currencies {
		curr := currency.(map[string]interface{})
		if curr["symbol"] == "AED" {
			aedCurrency = curr
		} else if curr["nameEn"] == "ALIPAY" { // Alipay currency ID
			alipayCurrency = curr
		}
	}
	if aedCurrency == nil || alipayCurrency == nil {
		color.Red("AED or Alipay currency not found\n")
		return
	}

	// Get current prices (swap buy and sell prices for AED)
	currentSellPrice, _ := strconv.ParseFloat(fmt.Sprintf("%v", aedCurrency["price"]), 64)
	currentPrice, _ := strconv.ParseFloat(fmt.Sprintf("%v", aedCurrency["sellPrice"]), 64)

	// Get Alipay price
	alipayPrice, _ := strconv.ParseFloat(fmt.Sprintf("%v", alipayCurrency["userPrice"]), 64)

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
			"Buy: %s\n\n",
		currentTime,
		formatNumber(adjustedSellPrice),
		formatNumber(adjustedBuyPrice),
	)

	// Display the terminal message
	color.Cyan("\n%s", terminalMessage)

	// Format the Telegram/WhatsApp message in Persian
	telegramMessage := fmt.Sprintf(
		"زمان (به وقت ایران): %s\n\n"+
			"درهم/تومان (حواله) 🇦🇪\n\n"+
			"%s :فروش\n"+
			"%s :خرید\n\n",
		currentTime,
		formatNumber(adjustedSellPrice),
		formatNumber(adjustedBuyPrice),
	)

	// Send the Telegram/WhatsApp message if it's the first run or prices have changed
	if *firstRun || currentPrice != lastPrice || currentSellPrice != lastSellPrice || alipayPrice != lastAlipayPrice {
		if !*firstRun && (currentPrice != lastPrice || currentSellPrice != lastSellPrice || alipayPrice != lastAlipayPrice) {
			// Send price change messages
			changeMessage := fmt.Sprintf(
				"زمان (به وقت ایران): %s\n\n"+
					"تغییر قیمت! 🚨\n\n"+
					"درهم/تومان (حواله) 🇦🇪\n\n"+
					"%s :فروش %s\n"+
					"%s :خرید %s\n\n",
				currentTime,
				formatNumber(adjustedSellPrice), getChangeSymbol(lastSellPrice, currentSellPrice),
				formatNumber(adjustedBuyPrice), getChangeSymbol(lastPrice, currentPrice),
			)
			sendTelegramMessage(changeMessage, telegramToken, chatID)
			sendWhatsAppMessage(changeMessage, config.WhatsAppList, config.WhatsAppBroadcastList) // Send to WhatsApp
		} else {
			// Send the main message (only on the first run)
			sendTelegramMessage(telegramMessage, telegramToken, chatID)
			sendWhatsAppMessage(telegramMessage, config.WhatsAppList, config.WhatsAppBroadcastList) // Send to WhatsApp
		}

		// Update the last prices
		lastPrice = currentPrice
		lastSellPrice = currentSellPrice
		lastAlipayPrice = alipayPrice

		// Mark the first run as completed
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

// Send a message to WhatsApp
func sendWhatsAppMessage(text string, whatsappList []string, broadcastList string) {
	client := resty.New()
	apiURL := "http://74.243.209.111:3000/api/sendText" // WhatsApp API URL

	// Send to individual WhatsApp numbers
	for _, chatID := range whatsappList {
		payload := map[string]interface{}{
			"chatId":      fmt.Sprintf("%s@c.us", chatID),
			"reply_to":    nil,
			"text":        text,
			"linkPreview": true,
			"session":     "default",
		}

		resp, err := client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(payload).
			Post(apiURL)

		if err != nil {
			color.Red("Error sending WhatsApp message to %s: %s\n", chatID, err)
		} else if resp.StatusCode() != 200 {
			color.Red("Failed to send WhatsApp message to %s: Status Code %d\n", chatID, resp.StatusCode())
		} else {
			color.Green("WhatsApp message sent successfully to %s!\n", chatID)
		}
	}

	// Send to the broadcast list (channel)
	if broadcastList != "" {
		payload := map[string]interface{}{
			"chatId":      broadcastList,
			"reply_to":    nil,
			"text":        text,
			"linkPreview": true,
			"session":     "default",
		}

		resp, err := client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(payload).
			Post(apiURL)

		if err != nil {
			color.Red("Error sending WhatsApp message to broadcast list (%s): %s\n", broadcastList, err)
		} else if resp.StatusCode() != 200 {
			color.Red("Failed to send WhatsApp message to broadcast list (%s): Status Code %d\n", broadcastList, resp.StatusCode())
		} else {
			color.Green("WhatsApp message sent successfully to broadcast list (%s)!\n", broadcastList)
		}
	}
}
