from Crypto.Cipher import DES

block = DES.new(secret, DES.MODE_ECB)
