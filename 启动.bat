@echo off
chcp 65001 >nul
cd /d "%~dp0"
title 发文字号取号系统
echo ==========================================
echo   发文字号取号系统
echo   访问地址 http://127.0.0.1:8080
echo   后台地址 http://127.0.0.1:8080/admin
echo   关闭此窗口即停止服务
echo ==========================================
echo.
use-number-windows-amd64.exe -port 8080
pause
