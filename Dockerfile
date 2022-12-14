FROM alpine:latest
RUN apk add tzdata
WORKDIR /app
COPY ./build/server ./server
COPY ./migrations ./migrations
CMD ["./server"]