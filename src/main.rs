use anyhow::Result;
use anicore::Bot;

#[tokio::main]
async fn main() -> Result<()> {
    // Загрузка переменных окружения из .env файла
    dotenv::dotenv().ok();

    // Инициализация логгера (он в anicore)
    anicore::init_logger();

    // Создание и запуск бота
    let bot = Bot::new().await?;
    bot.start().await?;

    Ok(())
}

