openssl req -new -nodes -x509 -out $1"bsrouter.pem" -keyout $1"bsrouter.key" -days 3650 -subj "/C=CN/ST=NRW/L=Earth/O=Random Company/OU=IT/CN=bsck.snows.io/emailAddress=bsck@snows.io"