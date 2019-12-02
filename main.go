package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/line/line-bot-sdk-go/linebot"
	"github.com/sminamot/nbanews"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/sheets/v4"
)

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

func main() {
	lambda.Start(HandleRequest)
}

// getClient uses a Context and Config to retrieve a Token
// then generate a Client. It returns the generated Client.
func getClient(ctx context.Context, config *oauth2.Config) (*http.Client, error) {
	base64token := os.Getenv("GOOGLE_TOKEN")
	var b []byte
	b, err := base64.StdEncoding.DecodeString(base64token)
	if err != nil {
		return nil, fmt.Errorf("Unable to decode base64 token from environment variable. %v", err)
	}
	br := bytes.NewReader(b)
	t := &oauth2.Token{}
	if err := json.NewDecoder(br).Decode(t); err != nil {
		return nil, fmt.Errorf("Unable to read token. %v", err)
	}
	expiry, _ := time.Parse("2006-01-02", "2017-07-11")
	t.Expiry = expiry
	return config.Client(ctx, t), nil
}

func HandleRequest() error {
	// fetch last news from spreadsheet
	//base64secret := os.Getenv("GOOGLE_SECRET")
	//secret, err := base64.StdEncoding.DecodeString(base64secret)
	secret, err := base64.StdEncoding.DecodeString(googleSecret)
	if err != nil {
		return fmt.Errorf("Unable to decode base64 client secret. %v", err)
	}

	config, err := google.ConfigFromJSON(secret, "https://www.googleapis.com/auth/spreadsheets")
	if err != nil {
		return fmt.Errorf("Unable to parse client secret file to config: %v", err)
	}
	ctx := context.Background()
	client, err := getClient(ctx, config)
	if err != nil {
		return err
	}

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
	//channelSecret := os.Getenv("LINE_CHANNEL_SECRET")
	//channelAccessToken := os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	channelID := os.Getenv("LINE_CHANNEL_ID")

	bot, err := linebot.New(channelSecret, channelAccessToken)
	if err != nil {
		return err
	}

	m := linebot.NewTextMessage(strings.Join(messages, "\n"))
	if _, err := bot.PushMessage(channelID, m).Do(); err != nil {
		return err
	}

	return nil
}
