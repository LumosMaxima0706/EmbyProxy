location = /admin {
    return 404;
}

location ^~ /admin/ {
    return 404;
}

location = /api/admin {
    return 404;
}

location ^~ /api/admin/ {
    return 404;
}

location ^~ /s/ {
    proxy_pass http://127.0.0.1:18081;
    proxy_http_version 1.1;
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Host $http_host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $stream_connection_upgrade;
    proxy_set_header Range $http_range;
    proxy_set_header If-Range $http_if_range;
    proxy_request_buffering off;
    proxy_buffering off;
    proxy_cache off;
    proxy_max_temp_file_size 0;
    proxy_connect_timeout 15s;
    proxy_send_timeout 3600s;
    proxy_read_timeout 3600s;
}
