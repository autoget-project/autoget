package notify

type INotifier interface {
	SendMessage(message string) error
}
