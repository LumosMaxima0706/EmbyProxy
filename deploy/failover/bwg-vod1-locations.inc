    location = /https/v1-vod1.uhdnow.com/443 {
        return 301 /https/v1-vod1.uhdnow.com/443/;
    }
    location ^~ /https/v1-vod1.uhdnow.com/443/ {
        proxy_pass http://127.0.0.1:18080;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $http_host;
        proxy_set_header X-Forwarded-Port $server_port;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $stream_bwg_connection_upgrade;
        proxy_set_header Range $http_range;
        proxy_set_header If-Range $http_if_range;
        proxy_set_header Authorization $http_authorization;
        proxy_set_header X-Emby-Authorization $http_x_emby_authorization;
        proxy_set_header X-Emby-Token $http_x_emby_token;
        proxy_set_header Cookie $http_cookie;
        proxy_set_header User-Agent $http_user_agent;
        proxy_set_header Accept $http_accept;
        proxy_set_header Accept-Encoding $http_accept_encoding;
        proxy_pass_header Content-Type;
        proxy_pass_header Content-Length;
        proxy_pass_header Content-Range;
        proxy_pass_header Accept-Ranges;
        proxy_pass_header Cache-Control;
        proxy_request_buffering off;
        proxy_buffering off;
        proxy_cache off;
        proxy_max_temp_file_size 0;
        proxy_connect_timeout 15s;
        proxy_send_timeout 3600s;
        proxy_read_timeout 3600s;
    }
