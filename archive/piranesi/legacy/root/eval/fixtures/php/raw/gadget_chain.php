<?php
class DeleteCache {
    public function __destruct() {
        unlink('/tmp/cache');
    }
}

unserialize($_COOKIE['payload']);
