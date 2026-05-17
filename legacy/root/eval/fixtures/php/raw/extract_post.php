<?php
extract($_POST);
if ($is_admin) {
    grant_admin();
}
