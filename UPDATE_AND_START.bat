@echo off
chcp 65001 >nul
title ArtBay Publisher - Update 4.0
cd /d "%~dp0"

echo ==============================================
echo   ArtBay Publisher 4.0 - Safe Setup and Queue
echo ==============================================
echo.
echo [1/4] Закрываю старые версии...
taskkill /F /IM ArtBayPublisher.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V2.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V3.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V3_1.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V3_2_FAST.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V3_3_STABLE.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V3_4_BULK.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V3_5_SKIP.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V3_6_CONTINUE.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V4.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V4_0_1.exe >nul 2>&1
taskkill /F /IM ArtBayPublisher_V4_0_2.exe >nul 2>&1
timeout /t 2 /nobreak >nul

echo [2/4] Проверяю порт 8765...
for /f "tokens=5" %%P in ('netstat -ano ^| findstr /R /C:":8765 .*LISTENING" 2^>nul') do (
    echo Найден старый процесс на порту 8765, PID %%P - закрываю.
    taskkill /F /PID %%P >nul 2>&1
)
timeout /t 1 /nobreak >nul

echo [3/4] Обновляю автозапуск, если он уже был включен...
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v ArtBayPublisher >nul 2>&1
if not errorlevel 1 (
    reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v ArtBayPublisher /t REG_SZ /d "\"%~dp0ArtBayPublisher.exe\" --background" /f >nul
)

echo [4/4] Запускаю ArtBay Publisher 4.0.4...
start "" "%~dp0ArtBayPublisher.exe"

echo.
echo Готово. Подключи Telegram и FunPay в открывшейся панели.
echo В Telegram команда /version должна показать v4.0.4.
timeout /t 5 >nul
