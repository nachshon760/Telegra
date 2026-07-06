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
)

func main() {
	// 1. יצירת לוגר מקצועי
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// 2. יצירת מפתח RSA פרטי וציבורי חדש בזיכרון עבור השרת
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate RSA key: %v", err)
	}

	// הדפסת המפתח הציבורי ללוגים כדי שתוכל להעתיק אותו לאנדרואיד סטודיו בקלות
	pubASN1, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubASN1,
	})
	fmt.Println("--- COPY THIS NEW KEY TO ANDROID STUDIO ---")
	fmt.Println(string(pubPEM))
	fmt.Println("-------------------------------------------")

	// 3. הגדרת שרת הבדיקות
	port := os.Getenv("PORT")
	if port == "" {
		port = "443"
	}

	// יצירת דיספאצ'ר שמטפל בפקודות (השפה של טלגרם)
	d := tgtest.NewDispatcher()

	// מימוש ה-Fallback Handler: כל פקודה שהשרת מקבל ולא מכיר, הוא יחזיר לה "הצלחה ריקה" כדי למנוע קריסה
	server := tgtest.NewServer(privateKey, tgtest.HandlerFunc(func(session tgtest.Session, in *bin.Buffer) error {
		id, err := in.PeekID()
		if err != nil {
			return err
		}

		// אם הדיספאצ'ר מכיר את הפקודה (כמו לוגין/הודעות) - בצע אותה
		if d.IsKnown(id) {
			return d.OnMessage(session, in)
		}

		// אם האפליקציה מבקשת פיצ'ר מתקדם שהשרת לא מממש באופן פעיל - מחזירים תגובה ריקה תקינה (True)
		logger.Debug("Handling unknown schema function with safe fallback", zap.Uint32("constructor_id", id))
		return session.ResultBlock(&tg.BoolTrue{})
	}))

	// הגדרת פקודות הליבה הבסיסיות (הרשמה, הגדרות ראשוניות)
	d.HandleFunc(tg.HelpGetConfigTypeID, func(session tgtest.Session, in *bin.Buffer) error {
		return session.ResultBlock(&tg.Config{})
	})

	// 4. הפעלת השרת כפרוטוקול תואם HTTP/Websocket עבור Cloud Run
	mux := http.NewServeMux()
	mux.HandleFunc("/tg", server.ServeHTTP)

	logger.Info("Telegram Private Server is running...", zap.String("port", port))
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		logger.Fatal("Server failed to start", zap.Error(err))
	}
}
