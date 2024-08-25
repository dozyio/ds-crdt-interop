FROM node:22-alpine

WORKDIR /app

RUN apk add yarn

COPY package.json yarn.lock ./

RUN yarn install

COPY . .

RUN yarn build

CMD ["npm", "start"]
