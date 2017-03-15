# Multiproxy

Proxy for duplicating requests to multiple backends in parallel.

## Configuration

    [graylog]
    addr = 127.0.0.1:12207
    
    [http]
    access_log = true
    addr = :9999
    keep_alive = 30
    
    [remote]
    hosts = http://127.0.0.1:8990;https://127.0.0.1:8991;http://127.0.0.1:8992
    backup = https://127.0.0.1:8993
	
Graylog section is optional. If present logs are written to Graylog.

Remote hosts are passed as string of addresses with ports delimited by semicolon.

## Troubleshooting

Start in console mode ommiting `-d` option. To see more detailed logs start with `-b` option.