module github.com/pyw0w/AniRuntime

go 1.25.5

require (
	github.com/bwmarrin/discordgo v0.29.0
	github.com/pyw0w/AniApi v0.1.0
	github.com/pyw0w/AniCore v0.1.0
)

require (
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/mattn/go-sqlite3 v1.14.32 // indirect
	golang.org/x/crypto v0.40.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
)

// Для локальной разработки (пути относительно AniRuntime)
replace github.com/pyw0w/AniApi => ../AniApi

replace github.com/pyw0w/AniCore => ..
