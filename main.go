package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sminamot/nbanews"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/sheets/v4"
)

// getClient uses a Context and Config to retrieve a Token
// then generate a Client. It returns the generated Client.
func getClient(ctx context.Context, config *oauth2.Config) *http.Client {
	base64token := os.Getenv("GOOGLE_TOKEN")
	var b []byte
	b, err := base64.StdEncoding.DecodeString(base64token)
	if err != nil {
		log.Fatalf("Unable to decode base64 token from environment variable. %v", err)
	}
	br := bytes.NewReader(b)
	t := &oauth2.Token{}
	if err := json.NewDecoder(br).Decode(t); err != nil {
		log.Fatalf("Unable to read token. %v", err)
	}
	expiry, _ := time.Parse("2006-01-02", "2017-07-11")
	t.Expiry = expiry
	return config.Client(ctx, t)
}

func main() {
	// fetch last news from spreadsheet
	base64secret := os.Getenv("GOOGLE_SECRET")
	secret, err := base64.StdEncoding.DecodeString(base64secret)
	if err != nil {
		log.Fatalf("Unable to decode base64 client secret. %v", err)
	}

	config, err := google.ConfigFromJSON(secret, "https://www.googleapis.com/auth/spreadsheets")
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	ctx := context.Background()
	client := getClient(ctx, config)

	srv, err := sheets.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve Sheets Client %v", err)
	}

	spreadsheetId := os.Getenv("SPREADSHEET_ID")
	readRange := "Sheet1!A1:B"
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetId, readRange).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve data from sheet. %v", err)
	}
	if len(resp.Values) == 0 {
		log.Fatal("No data found.")
	}

	lastNewsURL := ""
	for _, row := range resp.Values {
		lastNewsURL = row[1].(string)
	}

	// fetch news
	bk := nbanews.NewMediaBK()
	if err := bk.Fetch(); err != nil {
		log.Fatal(err)
	}

	al := bk.ArticleList()
	if len(al) == 0 {
		return
	}

	for _, a := range al {
		if a.URL == lastNewsURL {
			break
		}
		// TODO post
		fmt.Println(a.Title, a.URL)
	}

	var vr sheets.ValueRange
	vr.Values = append(vr.Values, []interface{}{bk.TargetURL(), al[0].URL})
	updateRange := "Sheet1!A1:B"
	_, err = srv.Spreadsheets.Values.Update(spreadsheetId, updateRange, &vr).ValueInputOption("RAW").Do()
	if err != nil {
		log.Fatalf("update error, %v", err)
	}
}
