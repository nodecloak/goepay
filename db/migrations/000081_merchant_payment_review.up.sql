-- Original Pro pay admission is independent from console account enabled.
-- Existing merchant columns, rows and all prior migration checksums remain.
ALTER TABLE merchants ADD COLUMN pay smallint NOT NULL DEFAULT 1
 CONSTRAINT merchant_pay_status CHECK(pay IN(0,1,2));
