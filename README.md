## NAME ##

pkup - IRC bot to organize pickup games

## SYNOPSIS ##

pkup [ **-n** *nick* ] [ **-r** *realname* ] [ **-u** *user* ] [ **-v** *vol* ] [ **-cc** *chan,...* ] *host:port* "*#channel*"


## DESCRIPTION ##

**Pkup** idles in the specified IRC channel and organizes pickup games between users.  It has been tested with Reflex, Quake 3, CPMA, and Warsow.  Support can easily be added for other games that support Quake or Source server status queries.

Ideally, the channel modes *nNmM* should be disabled.


## OPTIONS ##

**-n** *nick*  
The bot's nickname.  Default is "pickupbot`".

**-r** *realname*  
The bot's real name.  Default is "\\x0313pickupbot".  May contain spaces.

**-u** *user*  
The "user" part of "user@host".  Default is "pkup".

**-v** *vol*  
Intrusiveness of the bot's responses to user queries (e.g. !who).  1=message user, 2=notice user, 3=message channel, 4=notice channel.  Default is 2.  

**-cc** "*#chan1,#chan2,...*"  
Other channels to send *!promote* messages.  Comma-separated, no spaces.


## COMMANDS ##

Commands may be said in the channel or messaged directly to the bot.  Square brackets ([]) denote an optional parameter, and an ellipsis (...) denotes any number of trailing parameters.

### User commands ##

**!add** [ *(-)mode* ] ...  
Adds you to the specified game modes, or to all modes if no modes are specified.  Prefixing a mode with '-' will add you to every mode except that one.

**!expire** *duration*  
Sets your expiry time (e.g. "1h30m") for all modes.  You will be removed from all modes when the expiry time lapses.

**!help**  
Shows usage information.

**!lastgame**  
Shows information about the last pickup game that started.

**!list**  
Lists all servers.

**!modes**  
Lists all modes.

**!month**  
Shows the 10 most played modes over the past month.

**!promote** [ *mode* ] ...  
Sends a message to the channel (and associated channels, if any) asking people to add for mode.  If no mode is specified, it asks people to add for the most populated mode.

**!q** *server*  
Queries the server and shows the retrieved information.

**!remove** *mode* ...  
Removes you from the specified modes.

**!top**  
Shows the 10 most active players for all time.

**!top10**  
Shows the 10 most active players over the past week.

**!top25**  
Shows the 25 most active players over the past month.

**!version**  
Shows useless information.

**!week**  
Shows the 10 most played modes over the past week.

**!who** [ *mode* ] ...  
Shows the players !added for the specified modes.  If no modes are specified, shows all players added for all modes.

### Operator commands ###

**!addserver** *alias* *host*[*:port*][*;password*] *game* *mode*  
Adds a server under the alias *alias* to *mode*'s server pool.  *Game* must be the name of a game known to the bot (reflex, quake, cpma, warsow).  *Mode* is created with 1 player slot if it does not already exist.

The port number in a Reflex server address should be the *Steam port*, not the *game port*.  For example, given a server with the default configuration, that means the port in the server's address should be 25787 rather than 25797.  It is not necessary to specify the port for a server that is using default ports.

**!delserver** *alias*  
Removes the server *alias* from the server pool of all modes.

**!mode** *mode* *numplayers*  
Creates a new mode *mode* with *numplayers* or updates *numplayers* if *mode* already exists.

**!motd** *motd*  
Sets the message of the day, which appears in the topic after the mode listing.


## CONFIGURATION ##
Pkup creates two files in the working directory: **pickup.rc** and **pickuphistory.log**.  **pickup.rc** contains operator commands to run at startup (without the leading exclamation marks). **pickuphistory.log** contains game history to track the top players and game modes.


## EXAMPLE ##
```
<@you> !mode CTF 8
<@you> !mode TDM 6
<@you> !mode FFA 8
<@you> !addserver xerosawyer xerosawyer.com:25788 reflex ctf tdm ffa
<@you> !addserver turbopixel 27.50.77.102 reflex ffa
<@you> !addserver example example.com;nicepassword123 reflex tdm ctf
<@you> !add -ffa
<@you> !expire 1h30m
<@you> !promote
```
The operator commands above are saved to **pickup.rc** so that they take effect again if the bot is restarted.
