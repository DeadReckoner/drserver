package types

import "time"

// EmailLog mirrors a row in public.email_log. Append-only; written by the
// email package after each send attempt and used for dedupe and audit.
type EmailLog struct {
	ID                string    `gorm:"primaryKey;column:id" json:"id"`
	UserID            string    `gorm:"column:user_id;not null" json:"user_id"`
	Kind              string    `gorm:"column:kind;not null" json:"kind"`
	ToEmail           string    `gorm:"column:to_email;not null" json:"to_email"`
	Status            string    `gorm:"column:status;not null" json:"status"`
	ProviderMessageID string    `gorm:"column:provider_message_id" json:"provider_message_id,omitempty"`
	ErrorText         string    `gorm:"column:error_text" json:"error_text,omitempty"`
	DedupeKey         string    `gorm:"column:dedupe_key" json:"dedupe_key,omitempty"`
	CreatedAt         time.Time `gorm:"column:created_at;not null" json:"created_at"`
}

func (EmailLog) TableName() string { return "email_log" }
