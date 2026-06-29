-- Create user_ads_tokens table to store Google Ads refresh tokens
-- This table stores OAuth refresh tokens for Google Ads integration

CREATE TABLE IF NOT EXISTS billing.user_ads_tokens (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL REFERENCES billing.users(id) ON DELETE CASCADE,
    login_customer_id VARCHAR(255),
    refresh_token TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(user_id)
);

-- Create index for faster lookups
CREATE INDEX IF NOT EXISTS idx_user_ads_tokens_user_id ON billing.user_ads_tokens(user_id);

-- Create index for customer ID lookups
CREATE INDEX IF NOT EXISTS idx_user_ads_tokens_login_customer_id ON billing.user_ads_tokens(login_customer_id);