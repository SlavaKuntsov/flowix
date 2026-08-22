# Frontend (Next.js 14 + hls.js)

> Phase 7 — scaffold. После `npm install` команды ниже заработают.

## Запуск
```bash
cd frontend
npm install
npm run dev      # http://localhost:3000
npm run build && npm run start  # prod

# Через OrbStack (после создания Dockerfile)
docker compose -f deploy/docker-compose.yml up -d frontend
```

## Линт / формат
```bash
npm run lint          # eslint
npm run lint:fix      # eslint --fix
npm run format        # prettier --write .
npx prettier --check .
# Корень:
make lint-front
make fmt-front
```

## Zed IDE (при сохранении)
`.zed/settings.json:1` → `TypeScript`/`TSX` → `typescript-language-server` + `eslint`, `format_on_save: on`
- При `Cmd+S`: `eslint --fix` + `organizeImports` + `prettier`
- Рефактор: `F2` rename, `Cmd+.` quick fix

## Создать проект (если пусто)
```bash
cd frontend
npx create-next-app@latest . --typescript --eslint --tailwind --app --src-dir
npm install hls.js zustand
```
