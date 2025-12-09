module github.com/pyw0w/AniRuntime

go 1.25.5

require (
	github.com/bwmarrin/discordgo v0.28.1
	github.com/pyw0w/AniApi v0.1.0
	github.com/pyw0w/AniCore v0.1.0
)

require (
	github.com/gorilla/websocket v1.4.2 // indirect
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b // indirect
	golang.org/x/sys v0.0.0-20201119102817-f84b799fce68 // indirect
)

// Для локальной разработки (пути относительно AniRuntime)
replace github.com/pyw0w/AniApi => ../AniApi

replace github.com/pyw0w/AniCore => ..
