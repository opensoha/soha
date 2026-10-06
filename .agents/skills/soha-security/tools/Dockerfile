FROM mirror.gcr.io/library/node@sha256:b26b04c123d9ff8ab646ceb18b9d75a1173acf64b9a401094b906d27b29338d4
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/ocr
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --omit=dev && ./node_modules/.bin/ocr --version
USER node
ENTRYPOINT ["/opt/ocr/node_modules/.bin/ocr"]
