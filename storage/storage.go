package storage

import (
	storage "github.com/supabase-community/storage-go"
)


type Options struct {
	supabaseURL string
	supbaseKey string
	bucketID string
}

type bucket interface {
	getImage() error
	setImage() error
}

func NewClient() {
	store := storage.NewClient("", "", nil)


}



