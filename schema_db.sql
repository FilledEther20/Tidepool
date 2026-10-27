CREATE TABLE enterprise_banking_ledger (
    txn_id UUID PRIMARY KEY, account_id VARCHAR(50), client_id VARCHAR(50), txn_type VARCHAR(10), 
    amount DECIMAL(18,4), currency VARCHAR(4), status VARCHAR(20), channel VARCHAR(30), merchant_category_code INT,
    device_ip VARCHAR(45), geo_lat DECIMAL(9,6), risk_score DECIMAL(5,2), geo_long DECIMAL(9,6), aml_flag BOOLEAN,
    fee DECIMAL(10,2), balance_after DECIMAL(18,4), initiated_at TIMESTAMP, cleared_at TIMESTAMP, raw_metadata JSONB
);