<?php
$email = $request->input('email');
$user = User::where('email', $email)->first();
