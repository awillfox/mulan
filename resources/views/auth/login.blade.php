@extends('layouts.app')

@section('content')
<div class="container">

    <div class="row justify-content-center">
        <div class="col-md-8">
            <div class="card">

                <p class="card-header-title title is-centered block"> {{ __('Login') }}
                </p>

                <div class="card-content block">
                    <div class="content">
                        <form method="POST" action="{{ route('login') }}">
                            @csrf

                            <div class="form-group row block">
                                <label for="email">{{ __('E-Mail Address') }}</label>

                                <input id="email" type="email" class="input @error('email') is-danger @enderror block"
                                    name="email" value="{{ old('email') }}" required autocomplete="email" autofocus
                                    placeholder="Email">

                                @error('email')
                                <div class="notification is-danger block">
                                    <!-- <button class="delete" onclick="this.parentElement.style.display='none'"></button> -->
                                    {{ $message }}
                                </div>

                                @enderror

                            </div>

                            <div class="form-group row block">
                                <label for="password">{{ __('Password') }}</label>

                                <div class="col-md-6">

                                    <input id="password" type="password"
                                        class="input @error('email') is-danger @enderror" name="password"
                                        value="{{ old('email') }}" required autocomplete="current-password">
                                    @error('password')
                                    <span class="invalid-feedback" role="alert">
                                        <strong>{{ $message }}</strong>
                                    </span>
                                    @enderror
                                </div>
                            </div>


                            <div class="form-group row block">
                                <div class="col-md-6 offset-md-4">
                                    <label class="checkbox" for="remember">
                                        <input type="checkbox" name="remember" id="remember"
                                            {{ old('remember') ? 'checked' : '' }}>
                                        {{ __('Remember Me') }}
                                    </label>

                                </div>
                            </div>

                            <div class="form-group row mb-0">
                                <div class="col-md-8 offset-md-4">
                                    <button type="submit" class="button is-success">
                                        {{ __('Login') }}
                                    </button>

                                    @if (Route::has('password.request'))

                                    <button class="button is-ghost"> <a href="{{ route('password.request') }}">
                                            {{ __('Forgot Your Password?') }}
                                        </a></button>

                                    @endif
                                </div>
                            </div>
                        </form>
                    </div>

                </div>



            </div>



        </div>
    </div>
</div>
</div>
@endsection