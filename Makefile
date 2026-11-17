.PHONY: tidy run peer test build

tidy:
	go mod tidy

build:
	go build -o bin/eino-bot ./cmd/bot

run:
	go run ./cmd/bot

# 第二个 bot 实例，便于本地 A2A 联调（对应样例简历里的 8081）
peer:
	BOT_ID=bot-candidate-1 BOT_NAME=CandidateAgent PORT=8081 BOT_PUBLIC_URL=http://127.0.0.1:8081 \
	OWNER_ID=user-candidate SQLITE_PATH=./data/peer.db go run ./cmd/bot

# 样例简历里还有 8082/8083；需要真实 A2A 时再开
peer2:
	BOT_ID=bot-candidate-2 BOT_NAME=CandidateAgent2 PORT=8082 BOT_PUBLIC_URL=http://127.0.0.1:8082 \
	OWNER_ID=user-candidate-2 SQLITE_PATH=./data/peer2.db go run ./cmd/bot

peer3:
	BOT_ID=bot-candidate-3 BOT_NAME=CandidateAgent3 PORT=8083 BOT_PUBLIC_URL=http://127.0.0.1:8083 \
	OWNER_ID=user-candidate-3 SQLITE_PATH=./data/peer3.db go run ./cmd/bot

# A2A 人才招聘市场 Agent（skill 标签「人才招聘」），注册到招聘方 Registry
market:
	BOT_ID=bot-market-recruit BOT_NAME=TalentMarketAgent PORT=8090 \
	BOT_PUBLIC_URL=http://127.0.0.1:8090 \
	A2A_MARKET_RECRUIT_MODE=true \
	A2A_REGISTRY_URL=http://127.0.0.1:12120 \
	OWNER_ID=user-market SQLITE_PATH=./data/market.db \
	BOT_RESUME_ROOT=./data/resumes go run ./cmd/bot

test:
	go test ./...
