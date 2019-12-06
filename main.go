package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/line/line-bot-sdk-go/linebot"
	"github.com/sminamot/nbanews"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/sheets/v4"
)

/*
var (
	encryptedGoogleSecret       = os.Getenv("GOOGLE_SECRET")
	encryptedChannelSecret      = os.Getenv("LINE_CHANNEL_SECRET")
	encryptedChannelAccessToken = os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	googleSecret                string
	channelSecret               string
	channelAccessToken          string
)

func init() {
	kmsClient := kms.New(session.New())
	googleSecret = decryptCipher(kmsClient, encryptedGoogleSecret)
	channelSecret = decryptCipher(kmsClient, encryptedChannelSecret)
	channelAccessToken = decryptCipher(kmsClient, encryptedChannelAccessToken)
}

func decryptCipher(kmsClient *kms.KMS, encrypted string) string {
	decodedBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		panic(err)
	}
	input := &kms.DecryptInput{
		CiphertextBlob: decodedBytes,
	}
	response, err := kmsClient.Decrypt(input)
	if err != nil {
		panic(err)
	}
	// Plaintext is a byte array, so convert to string
	return string(response.Plaintext[:])
}
*/

func main() {
	lambda.Start(HandleRequest)
}

func HandleRequest() error {
	secret, err := base64.StdEncoding.DecodeString(os.Getenv("GOOGLE_SECRET"))
	if err != nil {
		return fmt.Errorf("Unable to decode base64 client secret. %v", err)
	}

	conf, err := google.JWTConfigFromJSON(secret, sheets.SpreadsheetsScope)
	if err != nil {
		log.Fatal(err)
	}

	client := conf.Client(context.Background())
	srv, err := sheets.New(client)
	if err != nil {
		return fmt.Errorf("Unable to retrieve Sheets Client %v", err)
	}

	spreadsheetId := os.Getenv("SPREADSHEET_ID")
	readRange := "Sheet1!A1:B"
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetId, readRange).Do()
	if err != nil {
		return fmt.Errorf("Unable to retrieve data from sheet. %v", err)
	}
	if len(resp.Values) == 0 {
		return fmt.Errorf("No data found.")
	}

	lastNewsURL := ""
	for _, row := range resp.Values {
		lastNewsURL = row[1].(string)
	}

	// fetch news
	bk := nbanews.NewMediaBK()
	if err := bk.Fetch(); err != nil {
		return err
	}

	al := bk.ArticleList()
	if len(al) == 0 {
		return nil
	}

	// update spreadsheet
	var vr sheets.ValueRange
	vr.Values = append(vr.Values, []interface{}{bk.TargetURL(), al[0].URL})
	updateRange := "Sheet1!A1:B"
	if _, err = srv.Spreadsheets.Values.Update(spreadsheetId, updateRange, &vr).ValueInputOption("RAW").Do(); err != nil {
		return fmt.Errorf("update error, %v", err)
	}

	// notify
	// create message
	messages := make([]string, 0, len(al)*3-1)
	for _, a := range al {
		if a.URL == lastNewsURL {
			break
		}
		messages = append(messages, a.Title, a.URL, "")
	}
	// exit if there is no new news
	if len(messages) == 0 {
		return nil
	}
	// remove the last empty character
	messages = messages[:len(messages)-1]

	// post message
	channelSecret := os.Getenv("LINE_CHANNEL_SECRET")
	channelAccessToken := os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	bot, err := linebot.New(channelSecret, channelAccessToken)
	if err != nil {
		return err
	}

	m := linebot.NewTextMessage(strings.Join(messages, "\n"))
	if _, err := bot.BroadcastMessage(m).Do(); err != nil {
		return err
	}

	return nil
}
