INSERT INTO urls (code, long_url) VALUES
    ('google',    'https://www.google.com'),
    ('youtube',   'https://www.youtube.com'),
    ('github',    'https://www.github.com'),
    ('wikipedia', 'https://www.wikipedia.org'),
    ('reddit',    'https://www.reddit.com')
ON CONFLICT (code) DO NOTHING;
