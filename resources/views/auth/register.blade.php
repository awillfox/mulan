@extends('layouts.app')

@section('content')
<div class="container">
    <div class="card">
        <p class="card-header-title title is-centered block">{{ __('Register') }}</p>
        <div class="card-content block">
            <div class="content">
                <form method="POST" action="{{ route('register') }}">
                    @csrf

                    <!-- Email -->
                    <div class="form-group row block">
                        <label for="email">{{ __('E-Mail Address') }}</label>
                        <input id="email" type="email" class="input @error('email') is-danger @enderror block"
                            name="email" value="{{ old('email') }}" required autofocus placeholder="Email">
                        @error('email')
                        <div class="notification is-danger block">
                            {{ $message }}
                        </div>
                        @enderror
                    </div>


                    <!-- Name -->
                    <div class="form-group row block">
                        <label for="name">{{ __('Name') }}</label>
                        <input id="name" type="text" class="input @error('name') is-danger @enderror block" name="name"
                            value="{{ old('name') }}" required placeholder="Mame">
                        @error('name')
                        <div class="notification is-danger block">
                            {{ $message }}
                        </div>
                        @enderror
                    </div>

                    <!-- Password -->
                    <div class="form-group row block">
                        <label for="password">{{ __('Password') }}</label>
                        <input id="password" type="password" class="input @error('password') is-danger @enderror block"
                            name="password" value="{{ old('password') }}" required placeholder="Password">
                    </div>

                    <!-- Password Confirmation -->
                    <div class="form-group row block">
                        <label for="password-confirm">{{ __('Password Confirmation') }}</label>
                        <input id="password-confirm" type="password"
                            class="input @error('password') is-danger @enderror block" name="password_confirmation"
                            required placeholder="Password">
                        @error('password')
                        <div class="notification is-danger block">
                            {{ $message }}
                        </div>
                        @enderror
                    </div>

                    <!-- Button -->

                    <div class="form-group row mb-0">
                        <div class="col-md-8 offset-md-4">
                            <button type="submit" class="button is-success">
                                {{ __('Register') }}
                            </button>

                        </div>
                    </div>

                </form>
            </div>
        </div>
    </div>
</div>
</div>

@endsection