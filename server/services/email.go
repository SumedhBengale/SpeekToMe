package services

import (
	"fmt"
	"html"
	log "speektome-server/privacylog"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ses"
)

// SendVerificationEmail sends a verification link to the target user email using AWS SES
func SendVerificationEmail(targetEmail, name, token string) error {
	senderEmail := os.Getenv("SES_SENDER_EMAIL")
	if senderEmail == "" {
		senderEmail = "noreply@speektome.com"
	}

	backendURL := os.Getenv("BACKEND_URL")
	if backendURL == "" {
		backendURL = "http://localhost:8000"
	}

	verificationLink := fmt.Sprintf("%s/auth/verify?token=%s", backendURL, token)
	subject := "Verify your SpeekTo.Me account"

	// Local development simulation fallback if AWS credentials are not set
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		log.Printf("\n==================================================")
		log.Printf("       [EMAIL VERIFICATION DEVELOPMENT SIMULATOR]")
		log.Printf("==================================================")
		log.Printf("To:      %s", targetEmail)
		log.Printf("Subject: %s", subject)
		log.Printf("Link:    %s", verificationLink)
		log.Printf("==================================================\n")
		return nil
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %v", err)
	}

	svc := ses.New(sess)

	escapedName := html.EscapeString(name)
	escapedLink := html.EscapeString(verificationLink)

	htmlBody := fmt.Sprintf(`
		<!DOCTYPE html>
		<html>
		<head>
			<style>
				body { font-family: sans-serif; background-color: #ffffee; color: #1a1a1a; margin: 0; padding: 40px; }
				.card { background: #fffdf9; border: 2px solid #1a1a1a; border-radius: 12px; padding: 30px; box-shadow: 4px 4px 0px #1a1a1a; max-width: 500px; margin: 0 auto; }
				h2 { font-family: sans-serif; color: #034f46; margin-top: 0; }
				p { line-height: 1.5; color: rgba(26,26,26,0.8); }
				.btn { display: inline-block; background-color: #f0d7ff; border: 2px solid #1a1a1a; border-radius: 6px; padding: 10px 20px; font-weight: bold; color: #1a1a1a; text-decoration: none; box-shadow: 2px 2px 0px #1a1a1a; margin-top: 20px; }
				.btn:hover { background-color: #e2c1fb; }
			</style>
		</head>
		<body>
			<div class="card">
				<h2>🎙️ Welcome to SpeekTo.Me, %s!</h2>
				<p>Thank you for signing up. Please click the button below to verify your email address and activate your transcription services.</p>
				<p><a href="%s" class="btn">Verify Email Address</a></p>
				<p style="margin-top: 30px; font-size: 12px; color: rgba(26,26,26,0.5);">If the button above does not work, copy and paste this link into your browser:<br>%s</p>
			</div>
		</body>
		</html>
	`, escapedName, escapedLink, escapedLink)

	textBody := fmt.Sprintf(
		"Hello %s,\n\nWelcome to SpeekTo.Me! Please verify your email by opening the following link in your browser:\n\n%s\n\nThank you,\nThe SpeekTo.Me Team",
		name, verificationLink,
	)

	input := &ses.SendEmailInput{
		Destination: &ses.Destination{
			ToAddresses: []*string{aws.String(targetEmail)},
		},
		Message: &ses.Message{
			Body: &ses.Body{
				Html: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(htmlBody),
				},
				Text: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(textBody),
				},
			},
			Subject: &ses.Content{
				Charset: aws.String("UTF-8"),
				Data:    aws.String(subject),
			},
		},
		Source: aws.String(senderEmail),
	}

	_, err = svc.SendEmail(input)
	if err != nil {
		return fmt.Errorf("failed to send SES email: %v", err)
	}

	return nil
}

// SendPasswordResetEmail sends a password reset link to the target user email using AWS SES
func SendPasswordResetEmail(targetEmail, name, token string) error {
	senderEmail := os.Getenv("SES_SENDER_EMAIL")
	if senderEmail == "" {
		senderEmail = "noreply@speektome.com"
	}

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:8080"
	}

	resetLink := fmt.Sprintf("%s/dashboard?reset_token=%s", frontendURL, token)
	subject := "Reset your SpeekTo.Me password"

	// Local development simulation fallback if AWS credentials are not set
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		log.Printf("\n==================================================")
		log.Printf("       [PASSWORD RESET DEVELOPMENT SIMULATOR]")
		log.Printf("==================================================")
		log.Printf("To:      %s", targetEmail)
		log.Printf("Subject: %s", subject)
		log.Printf("Link:    %s", resetLink)
		log.Printf("==================================================\n")
		return nil
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %v", err)
	}

	svc := ses.New(sess)

	escapedName := html.EscapeString(name)
	escapedLink := html.EscapeString(resetLink)

	htmlBody := fmt.Sprintf(`
		<!DOCTYPE html>
		<html>
		<head>
			<style>
				body { font-family: sans-serif; background-color: #ffffee; color: #1a1a1a; margin: 0; padding: 40px; }
				.card { background: #fffdf9; border: 2px solid #1a1a1a; border-radius: 12px; padding: 30px; box-shadow: 4px 4px 0px #1a1a1a; max-width: 500px; margin: 0 auto; }
				h2 { font-family: sans-serif; color: #034f46; margin-top: 0; }
				p { line-height: 1.5; color: rgba(26,26,26,0.8); }
				.btn { display: inline-block; background-color: #f0d7ff; border: 2px solid #1a1a1a; border-radius: 6px; padding: 10px 20px; font-weight: bold; color: #1a1a1a; text-decoration: none; box-shadow: 2px 2px 0px #1a1a1a; margin-top: 20px; }
				.btn:hover { background-color: #e2c1fb; }
			</style>
		</head>
		<body>
			<div class="card">
				<h2>Password Reset Request</h2>
				<p>Hello %s,</p>
				<p>We received a request to reset the password for your SpeekTo.Me account. Click the button below to choose a new password. This link is valid for 1 hour.</p>
				<p><a href="%s" class="btn">Reset Password</a></p>
				<p style="margin-top: 30px; font-size: 12px; color: rgba(26,26,26,0.5);">If you did not request this change, you can safely ignore this email.<br><br>Direct link: %s</p>
			</div>
		</body>
		</html>
	`, escapedName, escapedLink, escapedLink)

	textBody := fmt.Sprintf(
		"Hello %s,\n\nWe received a request to reset your SpeekTo.Me password. Please visit the following link to choose a new password (valid for 1 hour):\n\n%s\n\nIf you did not request this, you can safely ignore this message.\n\nThe SpeekTo.Me Team",
		name, resetLink,
	)

	input := &ses.SendEmailInput{
		Destination: &ses.Destination{
			ToAddresses: []*string{aws.String(targetEmail)},
		},
		Message: &ses.Message{
			Body: &ses.Body{
				Html: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(htmlBody),
				},
				Text: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(textBody),
				},
			},
			Subject: &ses.Content{
				Charset: aws.String("UTF-8"),
				Data:    aws.String(subject),
			},
		},
		Source: aws.String(senderEmail),
	}

	_, err = svc.SendEmail(input)
	if err != nil {
		return fmt.Errorf("failed to send SES email: %v", err)
	}

	return nil
}

