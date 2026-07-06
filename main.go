package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgtest"
	"go.uber.org/zap"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ctx := context.Background()

	// הגדרה משתנה הדרייב מחוץ לבלוק כדי שלא יפיל את השרת
	var driveService *drive.Service
	var err error

	// נסיונות קריאת הקובץ - אם נכשל, השרת ממשיך לעבוד כרגיל בזיכרון!
	if _, statErr := os.Stat("google-credentials.json"); statErr == nil {
		driveService, err = drive.NewService(ctx, option.WithCredentialsFile("google-credentials.json"))
		if err != nil {
			logger.Warn("⚠️ קובץ הדרייב נמצא אך נכשל בחיבור - השרת ממשיך במצב זיכרון בלבד", zap.Error(err))
		} else {
			logger.Info("✅ החיבור לגוגל דרייב הצליח באופן מלא!")
		}
	} else {
		logger.Warn("⚠️ קובץ google-credentials.json לא נמצא - השרת ממשיך במצב זיכרון בלבד")
	}

	// 2. יצירת מפתח ה-RSA של השרת
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate RSA key: %v", err)
	}

	pubASN1, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: pubASN1})
	fmt.Println("--- COPY THIS NEW KEY TO ANDROID STUDIO ---")
	fmt.Println(string(pubPEM))
	fmt.Println("-------------------------------------------")

	port := os.Getenv("PORT")
	if port == "" {
		port = "443"
	}

	d := tgtest.NewDispatcher()

	// 3. ה-Fallback Handler הבטוח שלא קורס לעולם
	server := tgtest.NewServer(privateKey, tgtest.HandlerFunc(func(session tgtest.Session, in *bin.Buffer) error {
		id, err := in.PeekID()
		if err != nil {
			return err
		}

		if d.IsKnown(id) {
			return d.OnMessage(session, in)
		}

		// שמירה לדרייב רק אם השירות פעיל ומחובר באמת
		if id == tg.UploadSaveFilePartTypeID && driveService != nil {
			logger.Info("Saving an incoming file part directly to Google Drive...")
			f := &drive.File{Name: "telegram_uploaded_file.dat"}
			_, _ = driveService.Files.Create(f).Context(ctx).Do()
		}

		return session.ResultBlock(&tg.BoolTrue{})
	}))

	d.HandleFunc(tg.HelpGetConfigTypeID, func(session tgtest.Session, in *bin.Buffer) error {
		return session.ResultBlock(&tg.Config{})
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/tg", server.ServeHTTP)

	logger.Info("Telegram Private Server is running...", zap.String("port", port))
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		logger.Fatal("Server failed to start", zap.Error(err))
	}
}
