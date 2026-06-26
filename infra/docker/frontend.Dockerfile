# Leo-One RMM — Frontend React/Vite (mode développement avec hot reload)
# Build context : ./frontend

FROM node:20-alpine

WORKDIR /app

# Copie les manifestes de dépendances en premier (cache Docker)
COPY package*.json ./
RUN npm install

# Copie le reste du code source
COPY . .

EXPOSE 5173

# Lance Vite en mode dev avec écoute sur toutes les interfaces
# (requis pour être accessible depuis l'hôte via le port mappé)
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
