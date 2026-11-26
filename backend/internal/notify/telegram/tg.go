package telegram

import (
	"context"

	"github.com/go-telegram/bot"

	"github.com/autoget-project/autoget/backend/internal/notify"
)

type Config struct {
	Token  string `yaml:"token"`
	ChatID string `yaml:"chat_id"`
}

var _ notify.INotifier = (*Notifier)(nil)

type Notifier struct {
	config *Config
	bot    *bot.Bot
}

func New(config *Config) (*Notifier, error) {
	b, err := bot.New(config.Token, bot.WithSkipGetMe())
	if err != nil {
		return nil, err
	}

	return &Notifier{
		config: config,
		bot:    b,
	}, nil
}

func (n *Notifier) SendMessage(message string) error {
	_, err := n.bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: n.config.ChatID,
		Text:   message,
	})

	return err
}
